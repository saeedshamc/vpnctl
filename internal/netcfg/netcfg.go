// Package netcfg snapshots and applies default routes, DNS and IPv6 settings.
package netcfg

import (
	"context"
	"fmt"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/state"
)

// ErrNotSupported is returned on non-Linux builds.
var ErrNotSupported = fmt.Errorf("netcfg: only supported on linux")

// Snapshot captures the current network configuration for later restore.
func Snapshot(ctx context.Context, cfg config.Config) (*state.Snapshot, error) {
	return snapshot(ctx, cfg)
}

// ApplyTunnelRoutes installs the vpnctl default route via tunName and DNS/IPv6 policy.
func ApplyTunnelRoutes(ctx context.Context, tunName string, cfg config.Config, snap *state.Snapshot) error {
	return applyTunnel(ctx, tunName, cfg, snap)
}

// Restore rolls the system back using a previously taken snapshot.
func Restore(ctx context.Context, snap *state.Snapshot) error {
	if snap == nil {
		return state.ErrNoSnapshot
	}
	return restore(ctx, snap)
}
