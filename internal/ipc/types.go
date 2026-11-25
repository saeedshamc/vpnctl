// Package ipc defines the unix-socket JSON protocol between clients and vpnctld.
package ipc

import "time"

// Wire protocol version. Bump when request/response shapes change incompatibly.
const ProtocolVersion = 1

// Method names accepted by vpnctld.
const (
	MethodPing    = "ping"
	MethodUp      = "up"
	MethodDown    = "down"
	MethodStatus  = "status"
	MethodTest    = "test"
	MethodCleanup = "cleanup"
)

// Exit / error codes shared with the CLI (README §7).
const (
	CodeOK             = 0
	CodePhoneADB       = 2
	CodeProxyDead      = 3
	CodeNetworkFailed  = 4
	CodePermission     = 5
	CodeBadRequest     = 6
	CodeInternal       = 7
	CodeAlreadyRunning = 8
)

// Request is one JSON line from client → daemon.
type Request struct {
	ID      string         `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	Version int            `json:"v"`
}

// Response is one JSON line from daemon → client.
type Response struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	Version int    `json:"v"`
}

// Error is a structured failure.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// UpParams optional overrides for MethodUp / MethodTest.
type UpParams struct {
	PhonePort int    `json:"phone_port,omitempty"`
	LocalPort int    `json:"local_port,omitempty"`
	ProxyType string `json:"proxy_type,omitempty"`
}

// StatusResult is returned by MethodStatus and embedded in Up result.
type StatusResult struct {
	State       string    `json:"state"`
	LocalPort   int       `json:"local_port,omitempty"`
	PhonePort   int       `json:"phone_port,omitempty"`
	ProxyType   string    `json:"proxy_type,omitempty"`
	TunName     string    `json:"tun_name,omitempty"`
	ExitIP      string    `json:"exit_ip,omitempty"`
	ExitCountry string    `json:"exit_country,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	UptimeSec   int64     `json:"uptime_sec,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	PhoneState  string    `json:"phone_state,omitempty"`
}

// TestResult is returned by MethodTest (probe only).
type TestResult struct {
	OK         bool   `json:"ok"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	PhoneState string `json:"phone_state"`
	Message    string `json:"message,omitempty"`
}

// PingResult is returned by MethodPing.
type PingResult struct {
	Pong    bool   `json:"pong"`
	Version string `json:"version"`
}
