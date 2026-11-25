// Package tunnel creates and supervises the TUN device + tun2socks process.
package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/saeedshamc/vpnctl/internal/config"
)

const (
	// DefaultTunName is the virtual interface name.
	DefaultTunName = "tun0"
	// TunAddress is the CGNAT-ish address assigned to the TUN (same idea as Clash/meta).
	TunAddress = "198.18.0.1"
	// TunPrefix is the prefix length for TunAddress.
	TunPrefix = 15
)

// Handle is a running tunnel (TUN + tun2socks).
type Handle struct {
	Name string
	PID  int

	mu     sync.Mutex
	cancel context.CancelFunc
	done   <-chan error
	bin    string
}

// Options control how the tunnel is started.
type Options struct {
	Name      string
	Tun2Socks string // absolute path; resolved if empty
	Cfg       config.Config
}

// ErrNotSupported is returned on non-Linux builds.
var ErrNotSupported = fmt.Errorf("tunnel: only supported on linux")

// Alive reports whether tun2socks is still running.
func (h *Handle) Alive() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done == nil {
		return false
	}
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// Wait blocks until tun2socks exits.
func (h *Handle) Wait() error {
	if h == nil || h.done == nil {
		return nil
	}
	return <-h.done
}

// Stop terminates tun2socks and best-effort deletes the TUN device.
func (h *Handle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-h.done:
	case <-timer.C:
	case <-ctx.Done():
	}
	return destroyDevice(ctx, h.Name)
}
