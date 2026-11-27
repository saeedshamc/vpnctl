// Command vpnctl-gui is the tray icon + local web UI for vpnctl.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saeedshamc/vpnctl/internal/browser"
	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/daemonize"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/paths"
	"github.com/saeedshamc/vpnctl/internal/platform"
	"github.com/saeedshamc/vpnctl/internal/singleinstance"
	"github.com/saeedshamc/vpnctl/internal/tray"
	"github.com/saeedshamc/vpnctl/internal/version"
	"github.com/saeedshamc/vpnctl/internal/webui"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		trayOnly = flag.Bool("tray-only", false, "start silently in the tray")
		wizard   = flag.Bool("wizard", false, "force first-run wizard")
		showVer  = flag.Bool("version", false, "print version")
	)
	flag.Parse()
	if *showVer {
		fmt.Println(version.String())
		return 0
	}
	if !platform.Supported() {
		fmt.Fprintln(os.Stderr, platform.UnsupportedReason())
		return 1
	}

	lock, err := singleinstance.Acquire("gui")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 0
	}
	defer lock.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ui, err := webui.New()
	if err != nil {
		log.Error("webui", "err", err)
		return 1
	}
	ui.ForceWizard = *wizard
	go func() {
		if err := ui.ListenAndServe(ctx); err != nil {
			log.Error("webui serve", "err", err)
			stop()
		}
	}()
	// Wait briefly for Addr.
	for i := 0; i < 50 && ui.Addr == ""; i++ {
		time.Sleep(20 * time.Millisecond)
	}

	openUI := func(hash string) {
		_ = browser.OpenApp(ui.URL(hash))
	}
	go monitorAutoConnect(ctx, log)

	var tr *tray.Item
	tr, err = tray.Start(tray.Callbacks{
		OnConnect: func() {
			if err := call(ctx, ipc.MethodUp, nil); err != nil {
				log.Error("connect", "err", err)
			}
			refreshTray(tr)
		},
		OnDisconnect: func() {
			if err := call(ctx, ipc.MethodDown, nil); err != nil {
				log.Error("disconnect", "err", err)
			}
			refreshTray(tr)
		},
		OnOpen:     func() { openUI("") },
		OnSettings: func() { openUI("settings") },
		OnQuit:     func() { stop() },
	})
	if err != nil {
		log.Warn("tray unavailable; opening window", "err", err)
		openUI("")
	} else {
		defer tr.Close()
		if !*trayOnly || *wizard {
			openUI("")
		}
		go pollTray(ctx, tr)
	}

	<-ctx.Done()
	return 0
}

func monitorAutoConnect(ctx context.Context, log *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var nextAttempt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cfg, err := config.Load()
		if err != nil {
			log.Error("auto-connect config", "err", err)
			continue
		}
		if !cfg.AutoConnect || time.Now().Before(nextAttempt) {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		resp, err := callResponse(callCtx, ipc.MethodStatus, nil)
		if err == nil {
			var status ipc.StatusResult
			if raw, marshalErr := json.Marshal(resp.Result); marshalErr == nil {
				err = json.Unmarshal(raw, &status)
			} else {
				err = marshalErr
			}
			if err == nil && (status.State == "connected" || status.State == "connecting") {
				cancel()
				nextAttempt = time.Time{}
				continue
			}
			if err == nil && status.PhoneState != "device" {
				cancel()
				continue
			}
		}
		if err == nil {
			_, err = callResponse(callCtx, ipc.MethodTest, nil)
		}
		if err == nil {
			_, err = callResponse(callCtx, ipc.MethodUp, nil)
		}
		cancel()
		if err != nil {
			log.Warn("auto-connect failed", "err", err)
			nextAttempt = time.Now().Add(30 * time.Second)
			continue
		}
		log.Info("auto-connect succeeded")
		nextAttempt = time.Time{}
	}
}

func call(ctx context.Context, method string, params map[string]any) error {
	_, err := callResponse(ctx, method, params)
	return err
}

func callResponse(ctx context.Context, method string, params map[string]any) (ipc.Response, error) {
	if err := daemonize.EnsureRunning(ctx, paths.SocketPath()); err != nil {
		return ipc.Response{}, err
	}
	c := &ipc.Client{Path: paths.SocketPath(), Timeout: 90 * time.Second}
	resp, err := c.Call(method, params)
	if err != nil {
		return ipc.Response{}, err
	}
	if !resp.OK && resp.Error != nil {
		return resp, resp.Error
	}
	if !resp.OK {
		return resp, fmt.Errorf("helper rejected %s request", method)
	}
	return resp, nil
}

func pollTray(ctx context.Context, tr *tray.Item) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	last := tr.CurrentState()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refreshTray(tr)
			current := tr.CurrentState()
			if current != last {
				switch current {
				case tray.StateOn:
					tr.Notify("vpnctl connected", "The phone VPN tunnel is active.")
				case tray.StateOff:
					tr.Notify("vpnctl disconnected", "The phone VPN tunnel is disconnected.")
				case tray.StateError:
					tr.Notify("vpnctl connection error", "The phone VPN tunnel needs attention.")
				}
				last = current
			}
		}
	}
}

func refreshTray(tr *tray.Item) {
	if tr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := daemonize.EnsureRunning(ctx, paths.SocketPath()); err != nil {
		tr.SetState(tray.StateError, err.Error())
		return
	}
	c := &ipc.Client{Path: paths.SocketPath(), Timeout: 3 * time.Second}
	resp, err := c.Call(ipc.MethodStatus, nil)
	if err != nil || !resp.OK {
		tr.SetState(tray.StateError, "helper unreachable")
		return
	}
	var st ipc.StatusResult
	raw, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(raw, &st)
	switch st.State {
	case "connected":
		detail := st.ExitIP
		if st.ExitCountry != "" {
			detail = detail + " " + st.ExitCountry
		}
		tr.SetState(tray.StateOn, detail)
	case "connecting":
		tr.SetState(tray.StateConnecting, "")
	case "error":
		tr.SetState(tray.StateError, st.LastError)
	default:
		tr.SetState(tray.StateOff, "")
	}
}
