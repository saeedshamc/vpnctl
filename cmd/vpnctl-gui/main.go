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

	var tr *tray.Item
	tr, err = tray.Start(tray.Callbacks{
		OnConnect: func() {
			_ = call(ctx, ipc.MethodUp, nil)
			refreshTray(tr)
		},
		OnDisconnect: func() {
			_ = call(ctx, ipc.MethodDown, nil)
			refreshTray(tr)
		},
		OnOpen:     func() { openUI("") },
		OnSettings: func() { openUI("") },
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

func call(ctx context.Context, method string, params map[string]any) error {
	if err := daemonize.EnsureRunning(ctx, paths.SocketPath()); err != nil {
		return err
	}
	c := &ipc.Client{Path: paths.SocketPath(), Timeout: 90 * time.Second}
	resp, err := c.Call(method, params)
	if err != nil {
		return err
	}
	if !resp.OK && resp.Error != nil {
		return resp.Error
	}
	return nil
}

func pollTray(ctx context.Context, tr *tray.Item) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refreshTray(tr)
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
