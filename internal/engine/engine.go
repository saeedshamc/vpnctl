// Package engine implements the privileged up/down/test/cleanup state machine.
package engine

import (
	"context"
	"encoding/json"
	"errors"
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

	mu         sync.Mutex
	watchEpoch uint64
	cfg        config.Config
	st         state.File
	tun        *tunnel.Handle
	watch      *watchdog.Checker
	started    time.Time
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
	e.mu.Lock()
	defer e.mu.Unlock()

	cfg, err := e.mergeParams(p)
	if err != nil {
		return ipc.TestResult{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	}
	activeForward := e.st.ConnState == state.StateConnected
	if activeForward && (cfg.LocalPort != e.st.LocalPort || cfg.PhonePort != e.st.PhonePort || string(cfg.ProxyType) != e.st.ProxyType) {
		return ipc.TestResult{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "disconnect before testing different proxy settings"}
	}
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
	if !activeForward {
		if err := e.ADB.Forward(ctx, cfg.LocalPort, cfg.PhonePort); err != nil {
			return ipc.TestResult{PhoneState: string(st)}, &ipc.Error{Code: ipc.CodePhoneADB, Message: err.Error()}
		}
	}
	pr := probe.ThroughProxy(ctx, cfg)
	var removeErr error
	if !activeForward {
		removeErr = e.ADB.RemoveForward(ctx, cfg.LocalPort)
	}
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
		if removeErr != nil {
			return tr, &ipc.Error{Code: ipc.CodeNetworkFailed, Message: errors.Join(fmt.Errorf("proxy probe: %s", pr.Message), fmt.Errorf("remove adb forward: %w", removeErr)).Error()}
		}
		return tr, &ipc.Error{Code: ipc.CodeProxyDead, Message: pr.Message}
	}
	if removeErr != nil {
		return tr, &ipc.Error{Code: ipc.CodeNetworkFailed, Message: "remove adb forward: " + removeErr.Error()}
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
		if err := e.teardownLocked(ctx, "cleanup before up"); err != nil {
			return e.statusLocked(ctx), &ipc.Error{Code: ipc.CodeNetworkFailed, Message: "previous connection could not be restored: " + err.Error()}
		}
	}

	cfg, err := e.mergeParams(p)
	if err != nil {
		return e.statusLocked(ctx), &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	}
	e.cfg = cfg
	e.setStateLocked(state.StateConnecting, "")
	e.st.LocalPort = cfg.LocalPort
	e.st.PhonePort = cfg.PhonePort
	e.st.ProxyType = string(cfg.ProxyType)
	if err := state.Save(e.StatePath, e.st); err != nil {
		return e.failLocked(ctx, ipc.CodeNetworkFailed, "save connection state: "+err.Error())
	}

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
		removeErr := e.ADB.RemoveForward(ctx, cfg.LocalPort)
		return e.failLocked(ctx, ipc.CodeProxyDead, errors.Join(fmt.Errorf("%s", pr.Message), removeErr).Error())
	}

	snap, err := netcfg.Snapshot(ctx, cfg)
	if err != nil {
		removeErr := e.ADB.RemoveForward(ctx, cfg.LocalPort)
		return e.failLocked(ctx, ipc.CodeNetworkFailed, errors.Join(fmt.Errorf("snapshot: %w", err), removeErr).Error())
	}
	e.st.Snapshot = snap
	e.st.TunName = tunnel.DefaultTunName
	if err := state.Save(e.StatePath, e.st); err != nil {
		removeErr := e.ADB.RemoveForward(ctx, cfg.LocalPort)
		e.st.Snapshot = nil
		e.st.TunName = ""
		return e.failLocked(ctx, ipc.CodeNetworkFailed, errors.Join(fmt.Errorf("save recovery snapshot: %w", err), removeErr).Error())
	}

	h, err := tunnel.Start(ctx, tunnel.Options{Cfg: cfg})
	if err != nil {
		rollbackErr := errors.Join(
			netcfg.Restore(ctx, snap),
			e.ADB.RemoveForward(ctx, cfg.LocalPort),
		)
		if rollbackErr == nil {
			e.st = state.NewDisconnected()
		}
		return e.failLocked(ctx, ipc.CodeNetworkFailed, errors.Join(fmt.Errorf("tunnel: %w", err), rollbackErr).Error())
	}
	e.tun = h
	e.st.TunName = h.Name
	e.st.Tun2SocksPID = h.PID
	if err := state.Save(e.StatePath, e.st); err != nil {
		rollbackErr := errors.Join(netcfg.Restore(ctx, snap), h.Stop(ctx), e.ADB.RemoveForward(ctx, cfg.LocalPort))
		e.tun = nil
		if rollbackErr == nil {
			e.st = state.NewDisconnected()
		}
		return e.failLocked(ctx, ipc.CodeNetworkFailed, errors.Join(fmt.Errorf("save tunnel state: %w", err), rollbackErr).Error())
	}

	if err := netcfg.ApplyTunnelRoutes(ctx, h.Name, cfg, snap); err != nil {
		rollbackErr := errors.Join(netcfg.Restore(ctx, snap), h.Stop(ctx), e.ADB.RemoveForward(ctx, cfg.LocalPort))
		e.tun = nil
		if rollbackErr == nil {
			e.st = state.NewDisconnected()
		}
		return e.failLocked(ctx, ipc.CodeNetworkFailed, errors.Join(fmt.Errorf("routes: %w", err), rollbackErr).Error())
	}

	e.started = time.Now().UTC()
	e.st.StartedAt = e.started
	e.st.ConnState = state.StateConnected
	e.st.LastError = ""
	e.st.ExitIP, e.st.ExitCountry = lookupExit(ctx)
	if err := state.Save(e.StatePath, e.st); err != nil {
		if rollbackErr := e.teardownLocked(ctx, "save connected state"); rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
		return e.statusLocked(ctx), &ipc.Error{Code: ipc.CodeNetworkFailed, Message: "save connected state: " + err.Error()}
	}

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
	e.watchEpoch++
	if e.watch != nil {
		e.watch.Stop()
		e.watch = nil
	}
	var errs []error
	catch := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	if e.st.Snapshot != nil {
		catch(netcfg.Restore(ctx, e.st.Snapshot))
	}
	if e.tun != nil {
		catch(e.tun.Stop(ctx))
		e.tun = nil
	} else if e.st.TunName != "" {
		var stopErr error
		if e.st.Tun2SocksPID > 0 {
			stopErr = tunnel.StopProcess(e.st.Tun2SocksPID, e.st.TunName)
			catch(stopErr)
		}
		if stopErr == nil {
			catch(tunnel.CleanupStaleDevice(ctx, e.st.TunName))
		}
	}

	if e.ADB != nil && e.st.LocalPort > 0 {
		catch(e.ADB.RemoveForward(ctx, e.st.LocalPort))
	}

	if err := errors.Join(errs...); err != nil {
		e.st.ConnState = state.StateError
		e.st.LastError = err.Error()
		if saveErr := state.Save(e.StatePath, e.st); saveErr != nil {
			err = errors.Join(err, fmt.Errorf("save recovery state: %w", saveErr))
		}
		return err
	}
	e.st = state.NewDisconnected()
	return state.Save(e.StatePath, e.st)
}

func (e *Engine) startWatchdogLocked() {
	if e.watch != nil {
		e.watch.Stop()
	}
	e.watchEpoch++
	epoch := e.watchEpoch
	e.watch = &watchdog.Checker{
		Interval: 5 * time.Second,
		ADB:      e.ADB,
		Cfg: func() config.Config {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.cfg
		},
		Tunnel: func() *tunnel.Handle {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.tun
		},
		Log: e.Log,
		OnFail: func(f watchdog.Failure) {
			e.Log.Error("watchdog triggered down", "reason", f.Reason)
			e.mu.Lock()
			defer e.mu.Unlock()
			if epoch != e.watchEpoch || e.st.ConnState != state.StateConnected {
				return
			}
			if err := e.teardownLocked(context.Background(), f.Reason); err != nil {
				f.Reason = errors.Join(fmt.Errorf("%s", f.Reason), fmt.Errorf("teardown: %w", err)).Error()
			}
			e.st.ConnState = state.StateError
			e.st.LastError = f.Reason
			if err := state.Save(e.StatePath, e.st); err != nil {
				e.st.LastError = errors.Join(fmt.Errorf("%s", e.st.LastError), fmt.Errorf("save watchdog error state: %w", err)).Error()
				e.Log.Error("save watchdog error state", "err", err)
			}
		},
	}
	e.watch.Start()
}

func (e *Engine) mergeParams(p ipc.UpParams) (config.Config, error) {
	// Reload from disk so `vpnctl config set` takes effect without restarting vpnctld.
	loaded, err := config.Load()
	if err != nil {
		return config.Config{}, fmt.Errorf("reload config: %w", err)
	}
	cfg := loaded
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
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("invalid connection settings: %w", err)
	}
	return cfg, nil
}

func (e *Engine) setStateLocked(s state.ConnState, errMsg string) {
	e.st.ConnState = s
	e.st.LastError = errMsg
}

func (e *Engine) failLocked(ctx context.Context, code int, msg string) (ipc.StatusResult, *ipc.Error) {
	e.Log.Error("up failed", "code", code, "err", msg)
	e.setStateLocked(state.StateError, msg)
	if err := state.Save(e.StatePath, e.st); err != nil {
		msg = errors.Join(fmt.Errorf("%s", msg), fmt.Errorf("save error state: %w", err)).Error()
		e.st.LastError = msg
	}
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
