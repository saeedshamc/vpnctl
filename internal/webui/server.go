// Package webui serves the embedded local dashboard on 127.0.0.1.
package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/daemonize"
	"github.com/saeedshamc/vpnctl/internal/install"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/paths"
)

// Server is the localhost UI.
type Server struct {
	Token          string
	Addr           string // filled after Listen
	ForceWizard    bool
	OnOpenSettings func()

	mu   sync.Mutex
	ln   net.Listener
	http *http.Server
}

// New creates a server with a random session token.
func New() (*Server, error) {
	tok, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	return &Server{Token: tok}, nil
}

// ListenAndServe binds 127.0.0.1:0 and serves until ctx cancel.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.Addr = ln.Addr().String()
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS()))))
	mux.HandleFunc("/api/status", s.withAuth(s.handleStatus))
	mux.HandleFunc("/api/test", s.withAuth(s.handleTest))
	mux.HandleFunc("/api/up", s.withAuth(s.handleUp))
	mux.HandleFunc("/api/down", s.withAuth(s.handleDown))
	mux.HandleFunc("/api/config", s.withAuth(s.handleConfig))
	mux.HandleFunc("/api/logs", s.withAuth(s.handleLogs))
	mux.HandleFunc("/api/wizard", s.withAuth(s.handleWizard))

	s.http = &http.Server{
		Handler:           s.secure(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdown)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// URL returns the dashboard URL including the session token.
func (s *Server) URL(hash string) string {
	if hash != "" && !strings.HasPrefix(hash, "#") {
		hash = "#" + hash
	}
	return fmt.Sprintf("http://%s/?token=%s%s", s.Addr, s.Token, hash)
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Scheme != "http" || parsed.User != nil ||
				!isLocalHost(parsed.Host) || !sameLocalOriginHost(parsed.Host, r.Host) ||
				parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func isLocalHost(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
		port = ""
		if strings.Contains(hostport, ":") {
			return false
		}
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return host == "127.0.0.1" || host == "localhost"
}

func sameLocalOriginHost(originHost, requestHost string) bool {
	originName, originPort := splitHostPort(originHost)
	requestName, requestPort := splitHostPort(requestHost)
	return originName == requestName && originPort == requestPort
}

func splitHostPort(hostport string) (string, string) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport, ""
	}
	return host, port
}

func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-Vpnctl-Token")
		if tok != s.Token {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := readIndex()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	html := strings.Replace(string(data), "<head>", "<head>\n  <meta name=\"vpnctl-token\" content=\""+s.Token+"\" />", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	resp, err := s.rpc(r.Context(), ipc.MethodStatus, nil)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !resp.OK {
		writeErr(w, http.StatusBadGateway, responseError(resp))
		return
	}
	writeJSON(w, resp.Result)
}

func (s *Server) handleUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST only")
		return
	}
	resp, err := s.rpc(r.Context(), ipc.MethodUp, nil)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !resp.OK {
		writeErr(w, http.StatusBadGateway, responseError(resp))
		return
	}
	writeJSON(w, resp.Result)
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	resp, err := s.rpc(r.Context(), ipc.MethodTest, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if !resp.OK {
		writeErr(w, http.StatusBadGateway, responseError(resp))
		return
	}
	writeJSON(w, resp.Result)
}

func (s *Server) handleDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST only")
		return
	}
	resp, err := s.rpc(r.Context(), ipc.MethodDown, nil)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !resp.OK {
		writeErr(w, http.StatusBadGateway, responseError(resp))
		return
	}
	writeJSON(w, resp.Result)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Load()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, cfg)
	case http.MethodPost:
		var cfg config.Config
		if err := decodeJSON(w, r, &cfg); err != nil {
			writeErr(w, 400, "bad json")
			return
		}
		cfg.Normalize()
		if err := cfg.Validate(); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		prev, err := config.Load()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if err := config.SaveUser(cfg); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if cfg.LaunchAtLogin != prev.LaunchAtLogin {
			if err := install.SyncAutostartFromConfig(cfg.LaunchAtLogin); err != nil {
				writeErr(w, http.StatusInternalServerError, "settings saved but failed to update autostart: "+err.Error())
				return
			}
		}
		if cfg.HelperAtBoot != prev.HelperAtBoot {
			if cfg.HelperAtBoot {
				err = install.ServiceEnable(r.Context())
			} else {
				err = install.ServiceDisable(r.Context())
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "settings saved but failed to update helper service: "+err.Error())
				return
			}
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		writeErr(w, 405, "GET or POST")
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	data, err := os.ReadFile(paths.LogPath())
	if err != nil {
		if !os.IsNotExist(err) {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"text": "(no log yet)"})
		return
	}
	if len(data) > 200_000 {
		data = data[len(data)-200_000:]
	}
	writeJSON(w, map[string]any{"text": string(data)})
}

func (s *Server) handleWizard(w http.ResponseWriter, r *http.Request) {
	marker, err := wizardMarkerPath()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		_, err := os.Stat(marker)
		if err != nil && !os.IsNotExist(err) {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"needed": s.ForceWizard || os.IsNotExist(err)})
	case http.MethodPost:
		var body struct {
			LaunchAtLogin bool `json:"launch_at_login"`
			HelperAtBoot  bool `json:"helper_at_boot"`
			AutoConnect   bool `json:"auto_connect"`
			Done          bool `json:"done"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, 400, "bad json")
			return
		}
		cfg, err := config.Load()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.LaunchAtLogin = body.LaunchAtLogin
		cfg.HelperAtBoot = body.HelperAtBoot
		cfg.AutoConnect = body.AutoConnect
		if err := config.SaveUser(cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := install.SyncAutostartFromConfig(cfg.LaunchAtLogin); err != nil {
			writeErr(w, http.StatusInternalServerError, "settings saved but failed to update autostart: "+err.Error())
			return
		}
		if cfg.HelperAtBoot {
			if err := install.ServiceEnable(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, "settings saved but failed to enable helper service: "+err.Error())
				return
			}
		} else {
			if err := install.ServiceDisable(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, "settings saved but failed to disable helper service: "+err.Error())
				return
			}
		}
		if body.Done {
			if err := os.WriteFile(marker, []byte("ok\n"), 0o644); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		writeErr(w, 405, "GET or POST")
	}
}

func (s *Server) rpc(ctx context.Context, method string, params map[string]any) (ipc.Response, error) {
	if err := daemonize.EnsureRunning(ctx, paths.SocketPath()); err != nil {
		return ipc.Response{}, err
	}
	c := &ipc.Client{Path: paths.SocketPath(), Timeout: 90 * time.Second}
	return c.Call(method, params)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func responseError(resp ipc.Response) string {
	if resp.Error != nil && resp.Error.Message != "" {
		return resp.Error.Message
	}
	return "helper rejected request"
}

func wizardMarkerPath() (string, error) {
	dir, err := paths.UserConfigDir()
	if err != nil {
		return "", err
	}
	return dir + string(os.PathSeparator) + "wizard.done", nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
