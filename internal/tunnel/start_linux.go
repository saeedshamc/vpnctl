//go:build linux

package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/paths"
)

// Start creates tun0 (if needed), launches bundled tun2socks, waits until the
// interface is up, and assigns TunAddress.
func Start(ctx context.Context, opt Options) (*Handle, error) {
	name := opt.Name
	if name == "" {
		name = DefaultTunName
	}
	bin := opt.Tun2Socks
	if bin == "" {
		var err error
		bin, err = paths.ResolveBinary("tun2socks")
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("tun2socks binary: %w", err)
	}

	// Clean leftover device from a previous crash before creating.
	_ = destroyDevice(ctx, name)

	proxyURL := proxyEndpoint(opt.Cfg)
	// Field lesson: this build wants --flag form (not -flag).
	args := []string{
		"--device", name,
		"--proxy", proxyURL,
		"--loglevel", "info",
	}

	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Keep ambient capabilities out; we already run as root inside vpnctld.
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start tun2socks: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	h := &Handle{
		Name:   name,
		PID:    cmd.Process.Pid,
		cancel: cancel,
		done:   done,
		bin:    bin,
	}

	// Wait until the interface appears and assign address.
	deadline := time.Now().Add(8 * time.Second)
	for {
		if !h.Alive() {
			cancel()
			return nil, fmt.Errorf("tun2socks exited before interface %s came up", name)
		}
		if ifaceExists(name) {
			break
		}
		if time.Now().After(deadline) {
			_ = h.Stop(context.Background())
			return nil, fmt.Errorf("timeout waiting for interface %s", name)
		}
		select {
		case <-ctx.Done():
			_ = h.Stop(context.Background())
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	if err := configureAddress(ctx, name); err != nil {
		_ = h.Stop(context.Background())
		return nil, err
	}
	slog.Info("tunnel up", "dev", name, "pid", h.PID, "proxy", proxyURL)
	return h, nil
}

func proxyEndpoint(cfg config.Config) string {
	host := fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)
	switch cfg.ProxyType {
	case config.ProxyHTTP:
		return "http://" + host
	default:
		return "socks5://" + host
	}
}

func ifaceExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

func configureAddress(ctx context.Context, name string) error {
	// ip link set tun0 up
	if err := runIP(ctx, "link", "set", name, "up"); err != nil {
		return err
	}
	// Replace any previous address, then add ours.
	_ = runIP(ctx, "addr", "flush", "dev", name)
	cidr := fmt.Sprintf("%s/%d", TunAddress, TunPrefix)
	if err := runIP(ctx, "addr", "add", cidr, "dev", name); err != nil {
		return fmt.Errorf("addr add: %w", err)
	}
	return nil
}

func destroyDevice(ctx context.Context, name string) error {
	if name == "" || !ifaceExists(name) {
		return nil
	}
	_ = runIP(ctx, "link", "set", name, "down")
	_ = runIP(ctx, "link", "delete", name)
	return nil
}

func runIP(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %v: %s (%w)", args, string(out), err)
	}
	return nil
}
