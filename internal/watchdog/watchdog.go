// Package watchdog periodically verifies the live tunnel and triggers rollback.
package watchdog

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/saeedshamc/vpnctl/internal/adb"
	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/probe"
	"github.com/saeedshamc/vpnctl/internal/tunnel"
)

// Failure describes why the watchdog wants a teardown.
type Failure struct {
	Reason string
}

// Checker is called on an interval while connected.
type Checker struct {
	Interval time.Duration
	ADB      *adb.Client
	Cfg      func() config.Config
	Tunnel   func() *tunnel.Handle
	OnFail   func(Failure)
	Log      *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

// Start begins background checks. Safe to call multiple times (restarts).
func (c *Checker) Start() {
	c.Stop()
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	go c.loop(ctx)
}

// Stop ends the background loop.
func (c *Checker) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Checker) loop(ctx context.Context) {
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if f := c.checkOnce(ctx); f != nil {
				c.Log.Warn("watchdog failure", "reason", f.Reason)
				if c.OnFail != nil {
					c.OnFail(*f)
				}
				return
			}
		}
	}
}

func (c *Checker) checkOnce(ctx context.Context) *Failure {
	if c.Tunnel != nil {
		h := c.Tunnel()
		if h == nil || !h.Alive() {
			return &Failure{Reason: "tun2socks process died"}
		}
	}
	if c.ADB != nil {
		st, _, err := c.ADB.PrimaryState(ctx)
		if err != nil {
			return &Failure{Reason: "adb check failed: " + err.Error()}
		}
		if st != adb.StateDevice {
			return &Failure{Reason: "phone not connected (adb state=" + string(st) + ")"}
		}
	}
	if c.Cfg != nil {
		cfg := c.Cfg()
		res := probe.ThroughProxy(ctx, cfg)
		if !res.OK {
			return &Failure{Reason: "proxy unhealthy: " + res.Message}
		}
	}
	return nil
}
