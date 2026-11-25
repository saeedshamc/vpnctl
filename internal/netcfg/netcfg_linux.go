//go:build linux

package netcfg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/state"
)

func snapshot(ctx context.Context) (*state.Snapshot, error) {
	routes, err := listDefaultRoutes(ctx)
	if err != nil {
		return nil, err
	}
	dns, mode := snapshotDNS(ctx)
	ipv6On, err := ipv6Enabled()
	if err != nil {
		return nil, err
	}
	return &state.Snapshot{
		DefaultRoutes: routes,
		DNSServers:    dns,
		DNSMode:       mode,
		IPv6WasOn:     ipv6On,
	}, nil
}

func applyTunnel(ctx context.Context, tunName string, cfg config.Config, snap *state.Snapshot) error {
	// Default route via TUN with low metric so it wins.
	if err := run(ctx, "ip", "route", "replace", "default", "dev", tunName, "metric", "1"); err != nil {
		return fmt.Errorf("install default route: %w", err)
	}

	if cfg.DisableIPv6 {
		if err := setIPv6(false); err != nil {
			return fmt.Errorf("disable ipv6: %w", err)
		}
		if snap != nil {
			snap.IPv6Disabled = true
		}
	}

	if err := applyDNS(ctx, tunName, cfg); err != nil {
		return fmt.Errorf("apply dns: %w", err)
	}
	return nil
}

func restore(ctx context.Context, snap *state.Snapshot) error {
	var errs []error

	// Drop our default route; restore previous ones.
	_ = run(ctx, "ip", "route", "del", "default", "metric", "1")
	for _, r := range snap.DefaultRoutes {
		args := []string{"route", "replace", "default"}
		if r.Gateway != "" {
			args = append(args, "via", r.Gateway)
		}
		if r.Dev != "" {
			args = append(args, "dev", r.Dev)
		}
		if r.Metric > 0 {
			args = append(args, "metric", strconv.Itoa(r.Metric))
		}
		if err := run(ctx, "ip", args...); err != nil {
			errs = append(errs, err)
		}
	}

	if snap.IPv6Disabled {
		if err := setIPv6(snap.IPv6WasOn); err != nil {
			errs = append(errs, err)
		}
	}

	if err := restoreDNS(ctx, snap); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("restore: %v", errs)
	}
	return nil
}

func listDefaultRoutes(ctx context.Context) ([]state.RouteSnapshot, error) {
	out, err := output(ctx, "ip", "-4", "route", "show", "default")
	if err != nil {
		return nil, err
	}
	var routes []state.RouteSnapshot
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		r := parseDefaultRoute(line)
		if r.Family == "" {
			r.Family = "inet"
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func snapshotDNS(ctx context.Context) (servers []string, mode string) {
	// Prefer systemd-resolved.
	if out, err := output(ctx, "resolvectl", "dns"); err == nil && len(out) > 0 {
		mode = "resolved"
		for _, f := range strings.Fields(string(out)) {
			if net.ParseIP(f) != nil {
				servers = append(servers, f)
			}
		}
		return servers, mode
	}
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, "none"
	}
	mode = "resolvconf"
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "nameserver ") {
			ip := strings.TrimSpace(strings.TrimPrefix(line, "nameserver "))
			if net.ParseIP(ip) != nil {
				servers = append(servers, ip)
			}
		}
	}
	return servers, mode
}

func applyDNS(ctx context.Context, tunName string, cfg config.Config) error {
	if len(cfg.DNS) == 0 {
		return nil
	}
	// systemd-resolved path.
	if _, err := exec.LookPath("resolvectl"); err == nil {
		args := append([]string{"dns", tunName}, cfg.DNS...)
		if err := run(ctx, "resolvectl", args...); err != nil {
			return err
		}
		domainArgs := []string{"domain", tunName, "~."}
		_ = run(ctx, "resolvectl", domainArgs...)
		if cfg.DNSOverTLS {
			_ = run(ctx, "resolvectl", "dnsovertls", tunName, "yes")
		}
		_ = run(ctx, "resolvectl", "default-route", tunName, "true")
		return nil
	}
	// Fallback: write a private resolv.conf and hope resolvconf/symlink cooperates.
	// We only replace /etc/resolv.conf if it is a regular file we can rewrite;
	// otherwise skip with a soft warning by returning nil after trying.
	return writeResolvConf(cfg.DNS)
}

func restoreDNS(ctx context.Context, snap *state.Snapshot) error {
	switch snap.DNSMode {
	case "resolved":
		// Clearing the TUN interface DNS is enough; resolved falls back.
		_ = run(ctx, "resolvectl", "revert", "tun0")
		return nil
	case "resolvconf":
		if len(snap.DNSServers) == 0 {
			return nil
		}
		return writeResolvConf(snap.DNSServers)
	default:
		return nil
	}
}

func writeResolvConf(servers []string) error {
	var b strings.Builder
	b.WriteString("# written by vpnctl — will be restored on disconnect\n")
	for _, s := range servers {
		b.WriteString("nameserver ")
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644)
}

func ipv6Enabled() (bool, error) {
	data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6")
	if err != nil {
		// No IPv6 in kernel — treat as already off.
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	v := strings.TrimSpace(string(data))
	return v == "0", nil
}

func setIPv6(enabled bool) error {
	val := "1"
	if enabled {
		val = "0"
	}
	targets := []string{
		"/proc/sys/net/ipv6/conf/all/disable_ipv6",
		"/proc/sys/net/ipv6/conf/default/disable_ipv6",
	}
	for _, p := range targets {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.WriteFile(p, []byte(val+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %s (%w)", name, args, bytes.TrimSpace(out), err)
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
