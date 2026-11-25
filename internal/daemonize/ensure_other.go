//go:build !linux

package daemonize

import (
	"context"
	"fmt"

	"github.com/saeedshamc/vpnctl/internal/platform"
)

// EnsureRunning refuses to start the helper outside Linux.
func EnsureRunning(ctx context.Context, socket string) error {
	_, _ = ctx, socket
	return fmt.Errorf("%s", platform.UnsupportedReason())
}
