// Command vpnctld is the privileged network helper for vpnctl.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/saeedshamc/vpnctl/internal/engine"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/paths"
	"github.com/saeedshamc/vpnctl/internal/platform"
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

	if !platform.Supported() {
		fmt.Fprintln(os.Stderr, platform.UnsupportedReason())
		return 1
	}

	log, logFile, err := newLogger(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		return 1
	}
	if logFile != nil {
		defer logFile.Close()
	}

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
		return 1
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
			res, err := eng.Test(ctx, ipc.UpParamsFromMap(req.Params))
			if err != nil {
				resp := ipc.ErrResp(req.ID, err.Code, err.Message)
				resp.Result = res
				return resp
			}
			return ipc.OKResp(req.ID, res)
		case ipc.MethodUp:
			res, err := eng.Up(ctx, ipc.UpParamsFromMap(req.Params))
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

func authLists() (uids, gids []uint32) {
	uids = []uint32{0}
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

func newLogger(level string) (*slog.Logger, *os.File, error) {
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
	opts := &slog.HandlerOptions{Level: lv}

	writers := []io.Writer{os.Stderr}
	var file *os.File
	if err := os.MkdirAll(filepath.Dir(paths.LogPath()), 0o755); err == nil {
		f, err := os.OpenFile(paths.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			file = f
			writers = append(writers, f)
		}
	}
	h := slog.NewTextHandler(io.MultiWriter(writers...), opts)
	return slog.New(h), file, nil
}
