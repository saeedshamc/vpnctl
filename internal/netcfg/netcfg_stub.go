//go:build !linux

package netcfg

import (
	"context"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/state"
)

func snapshot(ctx context.Context, cfg config.Config) (*state.Snapshot, error) {
	_ = ctx
	_ = cfg
	return nil, ErrNotSupported
}

func applyTunnel(ctx context.Context, tunName string, cfg config.Config, snap *state.Snapshot) error {
	_, _, _, _ = ctx, tunName, cfg, snap
	return ErrNotSupported
}

func restore(ctx context.Context, snap *state.Snapshot) error {
	_, _ = ctx, snap
	return ErrNotSupported
}
