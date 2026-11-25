//go:build linux

package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runPrivileged runs argv as root via pkexec when needed.
// argv[0] must be an absolute path for polkit-friendly tools when possible.
func runPrivileged(ctx context.Context, argv ...string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty command")
	}
	if os.Geteuid() == 0 {
		return run(ctx, argv[0], argv[1:]...)
	}
	pk, err := exec.LookPath("pkexec")
	if err != nil {
		return "", fmt.Errorf("pkexec not found; run as root or install polkit")
	}
	args := append([]string{}, argv...)
	return run(ctx, pk, args...)
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = out
		}
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return out, nil
}

func look(name string) (string, error) {
	return exec.LookPath(name)
}
