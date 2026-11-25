package ipc

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
)

// Handler processes one authenticated request.
type Handler func(ctx context.Context, cred PeerCred, req Request) Response

// Server is the vpnctld unix-socket listener.
type Server struct {
	Path         string
	Handler      Handler
	AllowedUIDs  []uint32
	AllowedGIDs  []uint32
	Log          *slog.Logger

	mu       sync.Mutex
	ln       net.Listener
	wg       sync.WaitGroup
	shutdown bool
}

// ListenAndServe binds the socket and serves until ctx is cancelled or Close.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if err := os.MkdirAll(dirOf(s.Path), 0o755); err != nil {
		return err
	}
	_ = os.Remove(s.Path)

	ln, err := net.Listen("unix", s.Path)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.Path, err)
	}
	// Only root + group members should reach us via peercred; still tighten perms.
	if err := os.Chmod(s.Path, 0o660); err != nil {
		_ = ln.Close()
		return err
	}

	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	s.Log.Info("ipc listening", "path", s.Path)

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			done := s.shutdown
			s.mu.Unlock()
			if done {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go s.serveConn(ctx, conn)
	}
}

// Close stops accepting and waits for in-flight handlers.
func (s *Server) Close() error {
	s.mu.Lock()
	s.shutdown = true
	ln := s.ln
	s.ln = nil
	s.mu.Unlock()
	var err error
	if ln != nil {
		err = ln.Close()
	}
	s.wg.Wait()
	_ = os.Remove(s.Path)
	return err
}

func (s *Server) serveConn(ctx context.Context, c net.Conn) {
	defer s.wg.Done()
	defer c.Close()

	uc, ok := c.(*net.UnixConn)
	if !ok {
		s.Log.Warn("non-unix connection rejected")
		return
	}
	cred, err := PeerCredentials(uc)
	if err != nil {
		s.Log.Warn("peercred failed", "err", err)
		return
	}
	if !AllowPeer(cred, s.AllowedUIDs, s.AllowedGIDs) {
		s.Log.Warn("peer denied", "peer", DescribeCred(cred))
		_ = WriteJSON(c, ErrResp("", CodePermission, "permission denied"))
		return
	}

	r := bufio.NewReader(c)
	for {
		req, err := ReadRequest(r)
		if err != nil {
			return
		}
		resp := s.Handler(ctx, cred, req)
		if resp.Version == 0 {
			resp.Version = ProtocolVersion
		}
		if resp.ID == "" {
			resp.ID = req.ID
		}
		if err := WriteJSON(c, resp); err != nil {
			return
		}
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
