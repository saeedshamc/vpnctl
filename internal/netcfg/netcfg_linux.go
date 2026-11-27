//go:build linux

package netcfg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/state"
)

func snapshot(ctx context.Context, cfg config.Config) (*state.Snapshot, error) {
	routes, err := listDefaultRoutes(ctx)
	if err != nil {
		return nil, err
	}
	dns, mode := snapshotDNS(ctx)
	ipv6On, err := ipv6Enabled()
	if err != nil {
		return nil, err
	}
	snap := &state.Snapshot{
		DefaultRoutes: routes,
		DNSServers:    dns,
		DNSMode:       mode,
		IPv6Disabled:  cfg.DisableIPv6,
		IPv6WasOn:     ipv6On,
	}
	if mode != "resolved" {
		if cfg.DNSOverTLS {
			return nil, fmt.Errorf("DNS-over-TLS requires systemd-resolved on this system")
		}
		original, fileMode, existed, err := captureResolvConf("/etc/resolv.conf")
		if err != nil {
			return nil, err
		}
		snap.DNSMode = "resolvconf"
		snap.ResolvConfCaptured = true
		snap.ResolvConfExisted = existed
		snap.ResolvConfOriginal = original
		snap.ResolvConfMode = uint32(fileMode.Perm())
		snap.ResolvConfWritten = resolvConfContents(cfg.DNS)
	}
	return snap, nil
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
	} else if snap != nil && snap.IPv6WasOn {
		if err := run(ctx, "ip", "-6", "route", "replace", "default", "dev", tunName, "metric", "1"); err != nil {
			return fmt.Errorf("install IPv6 default route: %w", err)
		}
	}

	if err := applyDNS(ctx, tunName, cfg, snap); err != nil {
		return fmt.Errorf("apply dns: %w", err)
	}
	return nil
}

func restore(ctx context.Context, snap *state.Snapshot) error {
	var errs []error

	// Drop our default route; restore previous ones.
	for _, family := range []string{"-4", "-6"} {
		tunRoutes, err := output(ctx, "ip", family, "route", "show", "default", "dev", "tun0", "metric", "1")
		if err != nil {
			errs = append(errs, err)
		} else if len(bytes.TrimSpace(tunRoutes)) > 0 {
			if err := run(ctx, "ip", family, "route", "del", "default", "dev", "tun0", "metric", "1"); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if snap.IPv6Disabled {
		if err := setIPv6(snap.IPv6WasOn); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range snap.DefaultRoutes {
		family := "-4"
		if r.Family == "inet6" {
			family = "-6"
		}
		args := []string{family, "route", "replace", "default"}
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

	if err := restoreDNS(ctx, snap); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("restore: %w", errors.Join(errs...))
	}
	return nil
}

func listDefaultRoutes(ctx context.Context) ([]state.RouteSnapshot, error) {
	var routes []state.RouteSnapshot
	for _, family := range []struct {
		flag string
		name string
	}{{"-4", "inet"}, {"-6", "inet6"}} {
		out, err := output(ctx, "ip", family.flag, "route", "show", "default")
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			r := parseDefaultRoute(line)
			r.Family = family.name
			routes = append(routes, r)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return routes, nil
}

func snapshotDNS(ctx context.Context) (servers []string, mode string) {
	// Prefer systemd-resolved.
	if _, err := exec.LookPath("resolvectl"); err == nil {
		out, err := output(ctx, "resolvectl", "dns")
		if err != nil {
			return nil, "none"
		}
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

func applyDNS(ctx context.Context, tunName string, cfg config.Config, snap *state.Snapshot) error {
	// systemd-resolved path.
	if snap != nil && snap.DNSMode == "resolved" {
		if len(cfg.DNS) == 0 {
			return fmt.Errorf("dns must contain at least one address")
		}
		args := append([]string{"dns", tunName}, cfg.DNS...)
		if err := run(ctx, "resolvectl", args...); err != nil {
			return err
		}
		domainArgs := []string{"domain", tunName, "~."}
		if err := run(ctx, "resolvectl", domainArgs...); err != nil {
			return err
		}
		tls := "no"
		if cfg.DNSOverTLS {
			tls = "yes"
		}
		if err := run(ctx, "resolvectl", "dnsovertls", tunName, tls); err != nil {
			return err
		}
		return run(ctx, "resolvectl", "default-route", tunName, "true")
	}
	if snap == nil || !snap.ResolvConfCaptured {
		return fmt.Errorf("missing resolv.conf recovery snapshot")
	}
	return writeResolvConfBytes("/etc/resolv.conf", resolvConfContents(cfg.DNS), os.FileMode(snap.ResolvConfMode))
}

func restoreDNS(ctx context.Context, snap *state.Snapshot) error {
	switch snap.DNSMode {
	case "resolved":
		// Revert before deleting the TUN device so resolved can address the link.
		return run(ctx, "resolvectl", "revert", "tun0")
	case "resolvconf":
		if snap.ResolvConfCaptured {
			return restoreResolvConf("/etc/resolv.conf", snap)
		}
		if len(snap.DNSServers) == 0 {
			return nil
		}
		return writeResolvConf("/etc/resolv.conf", snap.DNSServers)
	default:
		return nil
	}
}

func writeResolvConf(path string, servers []string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to overwrite non-regular file %s", path)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeResolvConfBytes(path, resolvConfContents(servers), mode)
}

func resolvConfContents(servers []string) []byte {
	var b strings.Builder
	b.WriteString("# written by vpnctl — will be restored on disconnect\n")
	for _, s := range servers {
		b.WriteString("nameserver ")
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func captureResolvConf(path string) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, 0o644, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("refusing to modify non-regular file %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	return data, info.Mode().Perm(), true, nil
}

func restoreResolvConf(path string, snap *state.Snapshot) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) && !snap.ResolvConfExisted {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to restore non-regular file %s", path)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, snap.ResolvConfWritten) {
		return fmt.Errorf("%s changed outside vpnctl; leaving the external DNS configuration intact", path)
	}
	if !snap.ResolvConfExisted {
		return os.Remove(path)
	}
	return writeResolvConfBytes(path, snap.ResolvConfOriginal, os.FileMode(snap.ResolvConfMode))
}

func writeResolvConfBytes(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to overwrite non-regular file %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".vpnctl-resolv.conf-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
