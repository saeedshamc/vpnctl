//go:build linux

package daemonize

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/saeedshamc/vpnctl/internal/paths"
)

// EnsureRunning makes sure vpnctld is listening on the socket.
// If not, it tries pkexec, then a plain absolute-path start (already-root cases).
func EnsureRunning(ctx context.Context, socket string) error {
	if socket == "" {
		socket = paths.SocketPath()
	}
	if ping(socket) {
		return nil
	}
	bin, err := resolveDaemon()
	if err != nil {
		return err
	}
	if err := startHelper(ctx, bin); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ping(socket) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return fmt.Errorf("vpnctld did not become ready on %s", socket)
}

func ping(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func resolveDaemon() (string, error) {
	candidates := []string{
		filepath.Join(paths.LibDir, "vpnctld"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "vpnctld"),
			filepath.Join(dir, "bin", "vpnctld"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "bin", "vpnctld"))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	if p, err := exec.LookPath("vpnctld"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("vpnctld binary not found (build it or install the package)")
}

func startHelper(ctx context.Context, bin string) error {
	// Prefer polkit password dialog.
	if pk, err := exec.LookPath("pkexec"); err == nil {
		cmd := exec.CommandContext(ctx, pk, bin, "--service")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("pkexec vpnctld: %w", err)
		}
		// Detach: we only needed the spawn; vpnctld keeps running.
		go func() { _ = cmd.Wait() }()
		return nil
	}
	// Fallback when already privileged / no polkit.
	cmd := exec.CommandContext(ctx, bin, "--service")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start vpnctld: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
