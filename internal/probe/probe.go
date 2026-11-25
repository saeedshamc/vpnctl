// Package probe checks the phone-side proxy without touching system routes.
package probe

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/saeedshamc/vpnctl/internal/config"
)

// Result is the outcome of a proxy health check.
type Result struct {
	OK        bool
	Latency   time.Duration
	Status    int
	Message   string
	WarnedEnv []string // proxy-related env vars that could fool curl-based tests
}

// CheckEnvProxyVars returns names of HTTP(S)/ALL_PROXY vars that are set.
// Per field-testing lesson: shell proxy env must never be trusted for health checks.
func CheckEnvProxyVars() []string {
	keys := []string{
		"http_proxy", "https_proxy", "all_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
		"no_proxy", "NO_PROXY",
	}
	var found []string
	for _, k := range keys {
		if os.Getenv(k) != "" {
			found = append(found, k)
		}
	}
	return found
}

// ThroughProxy fetches testURL via the local forwarded proxy.
// It never consults HTTP_PROXY env (explicit dialer).
func ThroughProxy(ctx context.Context, cfg config.Config) Result {
	warned := CheckEnvProxyVars()
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)

	start := time.Now()
	transport, err := proxyTransport(cfg.ProxyType, addr)
	if err != nil {
		return Result{OK: false, Message: err.Error(), WarnedEnv: warned}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.TestURL, nil)
	if err != nil {
		return Result{OK: false, Message: err.Error(), WarnedEnv: warned}
	}
	req.Header.Set("User-Agent", "vpnctl-probe/1")

	resp, err := client.Do(req)
	if err != nil {
		return Result{
			OK:        false,
			Latency:   time.Since(start),
			Message:   fmt.Sprintf("proxy probe failed: %v", err),
			WarnedEnv: warned,
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	ok := resp.StatusCode > 0 && resp.StatusCode < 500
	msg := fmt.Sprintf("HTTP %d via %s://%s", resp.StatusCode, cfg.ProxyType, addr)
	if !ok {
		msg = fmt.Sprintf("unexpected status %d from %s", resp.StatusCode, cfg.TestURL)
	}
	return Result{
		OK:        ok,
		Latency:   time.Since(start),
		Status:    resp.StatusCode,
		Message:   msg,
		WarnedEnv: warned,
	}
}

func proxyTransport(pt config.ProxyType, addr string) (*http.Transport, error) {
	switch pt {
	case config.ProxyHTTP:
		u, err := url.Parse("http://" + addr)
		if err != nil {
			return nil, err
		}
		return &http.Transport{
			Proxy:               http.ProxyURL(u),
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 10 * time.Second,
			// Explicit dialer — ignore env.
			DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ProxyConnectHeader: make(http.Header),
		}, nil
	default: // socks5
		dialer, err := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: 10 * time.Second})
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("socks dialer does not support context")
		}
		return &http.Transport{
			DialContext:           contextDialer.DialContext,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			// No Proxy set — traffic goes through SOCKS dialer only.
		}, nil
	}
}

// ProxyURL returns a URL string for logging (socks5h for remote DNS).
func ProxyURL(cfg config.Config) string {
	host := fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)
	switch cfg.ProxyType {
	case config.ProxyHTTP:
		return "http://" + host
	default:
		return "socks5h://" + host
	}
}

// FormatWarnedEnv builds a short warning for CLI/logs.
func FormatWarnedEnv(vars []string) string {
	if len(vars) == 0 {
		return ""
	}
	return "warning: proxy env vars set (" + strings.Join(vars, ", ") + "); probe ignores them"
}
