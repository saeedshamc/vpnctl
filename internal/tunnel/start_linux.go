//go:build linux

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

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

	if ifaceExists(name) {
		return nil, fmt.Errorf("TUN device %s already exists; refusing to replace an interface not owned by this process", name)
	}

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
		Name:    name,
		PID:     cmd.Process.Pid,
		cancel:  cancel,
		done:    done,
		bin:     bin,
		process: cmd.Process,
	}

	// Wait until the interface appears and assign address.
	deadline := time.Now().Add(8 * time.Second)
	for {
		if !h.Alive() {
			return nil, errors.Join(
				fmt.Errorf("tun2socks exited before interface %s came up", name),
				h.Stop(context.Background()),
			)
		}
		if ifaceExists(name) {
			break
		}
		if time.Now().After(deadline) {
			return nil, errors.Join(
				fmt.Errorf("timeout waiting for interface %s", name),
				h.Stop(context.Background()),
			)
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), h.Stop(context.Background()))
		case <-time.After(100 * time.Millisecond):
		}
	}

	if err := configureAddress(ctx, name); err != nil {
		return nil, errors.Join(err, h.Stop(context.Background()))
	}
	slog.Info("tunnel up", "dev", name, "pid", h.PID, "proxy", proxyURL)
	return h, nil
}

// StopProcess terminates a stale tun2socks process recorded in the recovery file.
func StopProcess(pid int, name string) error {
	if pid <= 1 || name == "" {
		return nil
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open stale tun2socks process %d: %w", pid, err)
	}
	defer unix.Close(fd)

	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(args) < 3 || filepath.Base(args[0]) != "tun2socks" {
		return nil
	}
	deviceFound := false
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--device" && args[i+1] == name {
			deviceFound = true
			break
		}
	}
	if !deviceFound {
		return nil
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("stop stale tun2socks process %d: %w", pid, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := unix.PidfdSendSignal(fd, 0, nil, 0)
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("check stale tun2socks process %d: %w", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("kill stale tun2socks process %d: %w", pid, err)
	}
	return nil
}

// CleanupStaleDevice removes a TUN device only when its address identifies vpnctl's interface.
func CleanupStaleDevice(ctx context.Context, name string) error {
	if name == "" || !ifaceExists(name) {
		return nil
	}
	out, err := exec.CommandContext(ctx, "ip", "-4", "addr", "show", "dev", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect stale TUN device %s: %s (%w)", name, strings.TrimSpace(string(out)), err)
	}
	if !strings.Contains(string(out), "inet 198.18.0.1/15") {
		return fmt.Errorf("refusing to remove TUN device %s: vpnctl address not found", name)
	}
	return destroyDevice(ctx, name)
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
	if err := runIP(ctx, "addr", "flush", "dev", name); err != nil {
		return fmt.Errorf("addr flush: %w", err)
	}
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
	if err := runIP(ctx, "link", "delete", name); err != nil && ifaceExists(name) {
		return err
	}
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
