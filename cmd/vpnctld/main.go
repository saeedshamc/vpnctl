// Command vpnctld is the privileged network helper for vpnctl.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/saeedshamc/vpnctl/internal/engine"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/paths"
	"github.com/saeedshamc/vpnctl/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		cleanup     = flag.Bool("cleanup", false, "roll back leftover network state and exit")
		service     = flag.Bool("service", false, "run as systemd service (same as default)")
		socketPath  = flag.String("socket", paths.SocketPath(), "unix socket path")
		statePath   = flag.String("state", paths.StatePath(), "state file path")
		logLevel    = flag.String("log-level", "info", "debug|info|warn|error")
	)
	flag.Parse()
	_ = service

	if *showVersion {
		fmt.Println(version.String())
		return 0
	}

	log := newLogger(*logLevel)

	if err := paths.EnsureRuntimeDir(); err != nil {
		log.Error("runtime dir", "err", err)
		return 1
	}

	eng, err := engine.New(log, *statePath)
	if err != nil {
		log.Error("engine init", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := eng.RecoverIfNeeded(ctx); err != nil {
		log.Error("recovery", "err", err)
		// Continue anyway — better to keep serving so an explicit down can retry.
	}

	if *cleanup {
		if err := eng.Cleanup(ctx); err != nil {
			log.Error("cleanup", "err", err)
			return 1
		}
		log.Info("cleanup done")
		return 0
	}

	uids, gids := authLists()
	srv := &ipc.Server{
		Path:        *socketPath,
		AllowedUIDs: uids,
		AllowedGIDs: gids,
		Log:         log,
		Handler:     makeHandler(eng, log),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		_ = eng.Cleanup(context.Background())
		_ = srv.Close()
		return 0
	case err := <-errCh:
		if err != nil {
			log.Error("ipc server", "err", err)
			_ = eng.Cleanup(context.Background())
			return 1
		}
		return 0
	}
}

func makeHandler(eng *engine.Engine, log *slog.Logger) ipc.Handler {
	return func(ctx context.Context, cred ipc.PeerCred, req ipc.Request) ipc.Response {
		log.Debug("rpc", "method", req.Method, "peer", ipc.DescribeCred(cred), "id", req.ID)
		switch req.Method {
		case ipc.MethodPing:
			return ipc.OKResp(req.ID, ipc.PingResult{Pong: true, Version: version.Version})
		case ipc.MethodStatus:
			return ipc.OKResp(req.ID, eng.Status(ctx))
		case ipc.MethodTest:
			res, err := eng.Test(ctx, upParams(req))
			if err != nil {
				resp := ipc.ErrResp(req.ID, err.Code, err.Message)
				resp.Result = res
				return resp
			}
			return ipc.OKResp(req.ID, res)
		case ipc.MethodUp:
			res, err := eng.Up(ctx, upParams(req))
			if err != nil {
				resp := ipc.ErrResp(req.ID, err.Code, err.Message)
				resp.Result = res
				return resp
			}
			return ipc.OKResp(req.ID, res)
		case ipc.MethodDown:
			res, err := eng.Down(ctx)
			if err != nil {
				resp := ipc.ErrResp(req.ID, err.Code, err.Message)
				resp.Result = res
				return resp
			}
			return ipc.OKResp(req.ID, res)
		case ipc.MethodCleanup:
			if err := eng.Cleanup(ctx); err != nil {
				return ipc.ErrResp(req.ID, ipc.CodeNetworkFailed, err.Error())
			}
			return ipc.OKResp(req.ID, map[string]any{"cleaned": true})
		default:
			return ipc.ErrResp(req.ID, ipc.CodeBadRequest, "unknown method: "+req.Method)
		}
	}
}

func upParams(req ipc.Request) ipc.UpParams {
	var p ipc.UpParams
	if req.Params == nil {
		return p
	}
	if v, ok := asInt(req.Params["phone_port"]); ok {
		p.PhonePort = v
	}
	if v, ok := asInt(req.Params["local_port"]); ok {
		p.LocalPort = v
	}
	if v, ok := req.Params["proxy_type"].(string); ok {
		p.ProxyType = v
	}
	return p
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	default:
		return 0, false
	}
}

func authLists() (uids, gids []uint32) {
	uids = []uint32{0} // root
	if u, err := user.Current(); err == nil {
		if id, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			uids = append(uids, uint32(id))
		}
	}
	if g, err := user.LookupGroup("vpnctl"); err == nil {
		if id, err := strconv.ParseUint(g.Gid, 10, 32); err == nil {
			gids = append(gids, uint32(id))
		}
	}
	return uids, gids
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}
