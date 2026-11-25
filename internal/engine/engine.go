// Package engine implements the privileged up/down/test/cleanup state machine.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/saeedshamc/vpnctl/internal/adb"
	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/netcfg"
	"github.com/saeedshamc/vpnctl/internal/probe"
	"github.com/saeedshamc/vpnctl/internal/state"
	"github.com/saeedshamc/vpnctl/internal/tunnel"
	"github.com/saeedshamc/vpnctl/internal/watchdog"
)

// Engine owns the live tunnel and recovery state.
type Engine struct {
	Log       *slog.Logger
	StatePath string
	ADB       *adb.Client

	mu      sync.Mutex
	cfg     config.Config
	st      state.File
	tun     *tunnel.Handle
	watch   *watchdog.Checker
	started time.Time
}

// New builds an engine with defaults loaded from disk.
func New(log *slog.Logger, statePath string) (*Engine, error) {
	if log == nil {
		log = slog.Default()
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	adbClient, err := adb.New()
	if err != nil {
		log.Warn("adb binary not found yet; connect will fail until it is available", "err", err)
	}
	st, err := state.Load(statePath)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		Log:       log,
		StatePath: statePath,
		ADB:       adbClient,
		cfg:       cfg,
		st:        st,
	}
	return e, nil
}

// RecoverIfNeeded rolls back leftover network state from a previous crash.
func (e *Engine) RecoverIfNeeded(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.st.NeedsRecovery() {
		return nil
	}
	e.Log.Warn("recovering leftover network state", "conn_state", e.st.ConnState)
	return e.teardownLocked(ctx, "crash recovery")
}

// Status returns the current connection status.
func (e *Engine) Status(ctx context.Context) ipc.StatusResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.statusLocked(ctx)
}

func (e *Engine) statusLocked(ctx context.Context) ipc.StatusResult {
	res := ipc.StatusResult{
		State:       string(e.st.ConnState),
		LocalPort:   e.st.LocalPort,
		PhonePort:   e.st.PhonePort,
		ProxyType:   e.st.ProxyType,
		TunName:     e.st.TunName,
		ExitIP:      e.st.ExitIP,
		ExitCountry: e.st.ExitCountry,
		StartedAt:   e.st.StartedAt,
		LastError:   e.st.LastError,
	}
	if !e.st.StartedAt.IsZero() && e.st.ConnState == state.StateConnected {
		res.UptimeSec = int64(time.Since(e.st.StartedAt).Seconds())
	}
	if e.ADB != nil {
		st, _, err := e.ADB.PrimaryState(ctx)
		if err == nil {
			res.PhoneState = string(st)
		}
	}
	return res
}

// Test probes the phone proxy without changing routes.
func (e *Engine) Test(ctx context.Context, p ipc.UpParams) (ipc.TestResult, *ipc.Error) {
	cfg := e.mergeParams(p)
	if e.ADB == nil {
		return ipc.TestResult{}, &ipc.Error{Code: ipc.CodePhoneADB, Message: "adb binary not found"}
	}
	st, _, err := e.ADB.PrimaryState(ctx)
	if err != nil {
		return ipc.TestResult{PhoneState: string(adb.StateUnknown)}, &ipc.Error{Code: ipc.CodePhoneADB, Message: err.Error()}
	}
	if st != adb.StateDevice {
		msg := fmt.Sprintf("phone not ready (adb state=%s)", st)
		return ipc.TestResult{PhoneState: string(st), Message: msg}, &ipc.Error{Code: ipc.CodePhoneADB, Message: msg}
	}
	if err := e.ADB.Forward(ctx, cfg.LocalPort, cfg.PhonePort); err != nil {
		return ipc.TestResult{PhoneState: string(st)}, &ipc.Error{Code: ipc.CodePhoneADB, Message: err.Error()}
	}
	pr := probe.ThroughProxy(ctx, cfg)
	tr := ipc.TestResult{
		OK:         pr.OK,
		LatencyMS:  pr.Latency.Milliseconds(),
		PhoneState: string(st),
		Message:    pr.Message,
	}
	if w := probe.FormatWarnedEnv(pr.WarnedEnv); w != "" {
		tr.Message = tr.Message + "; " + w
	}
	if !pr.OK {
		return tr, &ipc.Error{Code: ipc.CodeProxyDead, Message: pr.Message}
	}
	return tr, nil
}

// Up brings the system-wide tunnel up (idempotent).
func (e *Engine) Up(ctx context.Context, p ipc.UpParams) (ipc.StatusResult, *ipc.Error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.st.ConnState == state.StateConnected && e.tun != nil && e.tun.Alive() {
		return e.statusLocked(ctx), nil
	}
	// Half-connected / error: clean first.
	if e.st.NeedsRecovery() || e.st.ConnState == state.StateConnecting || e.tun != nil {
		_ = e.teardownLocked(ctx, "cleanup before up")
	}

	cfg := e.mergeParams(p)
	e.setStateLocked(state.StateConnecting, "")
	e.st.LocalPort = cfg.LocalPort
	e.st.PhonePort = cfg.PhonePort
	e.st.ProxyType = string(cfg.ProxyType)
	_ = state.Save(e.StatePath, e.st)

	if e.ADB == nil {
		return e.failLocked(ctx, ipc.CodePhoneADB, "adb binary not found")
	}
	if _, err := e.ADB.RequireDevice(ctx); err != nil {
		return e.failLocked(ctx, ipc.CodePhoneADB, err.Error())
	}
	if err := e.ADB.Forward(ctx, cfg.LocalPort, cfg.PhonePort); err != nil {
		return e.failLocked(ctx, ipc.CodePhoneADB, err.Error())
	}

	// Probe BEFORE touching routes (README §5.1 step 4).
	pr := probe.ThroughProxy(ctx, cfg)
	if w := probe.FormatWarnedEnv(pr.WarnedEnv); w != "" {
		e.Log.Warn(w)
	}
	if !pr.OK {
		_ = e.ADB.RemoveForward(ctx, cfg.LocalPort)
		return e.failLocked(ctx, ipc.CodeProxyDead, pr.Message)
	}

	snap, err := netcfg.Snapshot(ctx)
	if err != nil {
		_ = e.ADB.RemoveForward(ctx, cfg.LocalPort)
		return e.failLocked(ctx, ipc.CodeNetworkFailed, "snapshot: "+err.Error())
	}
	e.st.Snapshot = snap
	_ = state.Save(e.StatePath, e.st)

	h, err := tunnel.Start(ctx, tunnel.Options{Cfg: cfg})
	if err != nil {
		_ = netcfg.Restore(ctx, snap)
		_ = e.ADB.RemoveForward(ctx, cfg.LocalPort)
		e.st.Snapshot = nil
		return e.failLocked(ctx, ipc.CodeNetworkFailed, "tunnel: "+err.Error())
	}
	e.tun = h
	e.st.TunName = h.Name
	e.st.Tun2SocksPID = h.PID
	_ = state.Save(e.StatePath, e.st)

	if err := netcfg.ApplyTunnelRoutes(ctx, h.Name, cfg, snap); err != nil {
		_ = h.Stop(ctx)
		e.tun = nil
		_ = netcfg.Restore(ctx, snap)
		_ = e.ADB.RemoveForward(ctx, cfg.LocalPort)
		e.st.Snapshot = nil
		return e.failLocked(ctx, ipc.CodeNetworkFailed, "routes: "+err.Error())
	}

	e.started = time.Now().UTC()
	e.st.StartedAt = e.started
	e.st.ConnState = state.StateConnected
	e.st.LastError = ""
	e.st.ExitIP, e.st.ExitCountry = lookupExit(ctx)
	_ = state.Save(e.StatePath, e.st)

	e.startWatchdogLocked()
	e.Log.Info("connected", "exit_ip", e.st.ExitIP, "country", e.st.ExitCountry)
	return e.statusLocked(ctx), nil
}

// Down tears the tunnel down and restores the network.
func (e *Engine) Down(ctx context.Context) (ipc.StatusResult, *ipc.Error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.teardownLocked(ctx, "user down"); err != nil {
		return e.statusLocked(ctx), &ipc.Error{Code: ipc.CodeNetworkFailed, Message: err.Error()}
	}
	return e.statusLocked(ctx), nil
}

// Cleanup is an alias used by systemd ExecStopPost / --cleanup.
func (e *Engine) Cleanup(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.teardownLocked(ctx, "cleanup")
}

func (e *Engine) teardownLocked(ctx context.Context, reason string) error {
	e.Log.Info("teardown", "reason", reason)
	if e.watch != nil {
		e.watch.Stop()
		e.watch = nil
	}
	var first error
	catch := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}

	if e.tun != nil {
		catch(e.tun.Stop(ctx))
		e.tun = nil
	} else if e.st.TunName != "" {
		// Best-effort: Stop on a synthetic handle deletes the device.
		h := &tunnel.Handle{Name: e.st.TunName}
		catch(h.Stop(ctx))
	}

	if e.st.Snapshot != nil {
		catch(netcfg.Restore(ctx, e.st.Snapshot))
	}
	if e.ADB != nil && e.st.LocalPort > 0 {
		catch(e.ADB.RemoveForward(ctx, e.st.LocalPort))
	}

	e.st = state.NewDisconnected()
	catch(state.Save(e.StatePath, e.st))
	return first
}

func (e *Engine) startWatchdogLocked() {
	if e.watch != nil {
		e.watch.Stop()
	}
	e.watch = &watchdog.Checker{
		Interval: 5 * time.Second,
		ADB:      e.ADB,
		Cfg:      func() config.Config { return e.cfg },
		Tunnel:   func() *tunnel.Handle { return e.tun },
		Log:      e.Log,
		OnFail: func(f watchdog.Failure) {
			e.Log.Error("watchdog triggered down", "reason", f.Reason)
			e.mu.Lock()
			defer e.mu.Unlock()
			_ = e.teardownLocked(context.Background(), f.Reason)
			e.st.ConnState = state.StateError
			e.st.LastError = f.Reason
			_ = state.Save(e.StatePath, e.st)
		},
	}
	e.watch.Start()
}

func (e *Engine) mergeParams(p ipc.UpParams) config.Config {
	// Reload from disk so `vpnctl config set` takes effect without restarting vpnctld.
	if loaded, err := config.Load(); err == nil {
		e.cfg = loaded
	}
	cfg := e.cfg
	if p.PhonePort > 0 {
		cfg.PhonePort = p.PhonePort
	}
	if p.LocalPort > 0 {
		cfg.LocalPort = p.LocalPort
	}
	if p.ProxyType != "" {
		cfg.ProxyType = config.ProxyType(p.ProxyType)
	}
	cfg.Normalize()
	e.cfg = cfg
	return cfg
}

func (e *Engine) setStateLocked(s state.ConnState, errMsg string) {
	e.st.ConnState = s
	e.st.LastError = errMsg
}

func (e *Engine) failLocked(ctx context.Context, code int, msg string) (ipc.StatusResult, *ipc.Error) {
	e.Log.Error("up failed", "code", code, "err", msg)
	e.setStateLocked(state.StateError, msg)
	_ = state.Save(e.StatePath, e.st)
	return e.statusLocked(ctx), &ipc.Error{Code: code, Message: msg}
}

func lookupExit(ctx context.Context) (ip, country string) {
	// Best-effort; must not go through env proxy. Use a short timeout.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
	if err != nil {
		return "", ""
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", ""
	}
	var payload struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	return payload.IP, payload.Country
}
