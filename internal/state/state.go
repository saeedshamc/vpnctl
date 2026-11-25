// Package state persists a network snapshot for crash recovery.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/saeedshamc/vpnctl/internal/paths"
)

// ConnState is the high-level connection status.
type ConnState string

const (
	StateDisconnected ConnState = "disconnected"
	StateConnecting   ConnState = "connecting"
	StateConnected    ConnState = "connected"
	StateError        ConnState = "error"
)

// RouteSnapshot is one IPv4/IPv6 route that must be restored on down/crash.
type RouteSnapshot struct {
	Dst     string `json:"dst"`
	Gateway string `json:"gateway,omitempty"`
	Dev     string `json:"dev,omitempty"`
	Metric  int    `json:"metric,omitempty"`
	Table   int    `json:"table,omitempty"`
	Family  string `json:"family"` // "inet" | "inet6"
}

// Snapshot is everything needed to roll the PC network back.
type Snapshot struct {
	DefaultRoutes []RouteSnapshot `json:"default_routes"`
	DNSServers    []string        `json:"dns_servers,omitempty"`
	DNSMode       string          `json:"dns_mode,omitempty"` // "resolved" | "resolvconf" | "none"
	IPv6Disabled  bool            `json:"ipv6_disabled"`
	IPv6WasOn     bool            `json:"ipv6_was_on"`
}

// File is the on-disk recovery document under /run/vpnctl/state.json.
type File struct {
	Version    int       `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
	ConnState  ConnState `json:"conn_state"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	LocalPort  int       `json:"local_port,omitempty"`
	PhonePort  int       `json:"phone_port,omitempty"`
	ProxyType  string    `json:"proxy_type,omitempty"`
	TunName    string    `json:"tun_name,omitempty"`
	Tun2SocksPID int     `json:"tun2socks_pid,omitempty"`
	ExitIP     string    `json:"exit_ip,omitempty"`
	ExitCountry string   `json:"exit_country,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
	Snapshot   *Snapshot `json:"snapshot,omitempty"`
}

const currentVersion = 1

// NewDisconnected returns a fresh idle state document.
func NewDisconnected() File {
	return File{
		Version:   currentVersion,
		UpdatedAt: time.Now().UTC(),
		ConnState: StateDisconnected,
	}
}

// Path returns the default state file path.
func Path() string {
	return paths.StatePath()
}

// Load reads the state file. Missing file returns NewDisconnected() and nil error.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDisconnected(), nil
		}
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, err
	}
	if f.Version == 0 {
		f.Version = currentVersion
	}
	return f, nil
}

// Save writes f atomically (temp + rename) so a crash mid-write cannot leave
// a half-parsed document.
func Save(path string, f File) error {
	f.Version = currentVersion
	f.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Clear removes the state file. Missing is OK.
func Clear(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// NeedsRecovery reports whether a leftover snapshot must be rolled back.
func (f File) NeedsRecovery() bool {
	if f.Snapshot == nil {
		return false
	}
	switch f.ConnState {
	case StateConnecting, StateConnected, StateError:
		return true
	default:
		return false
	}
}

// ErrNoSnapshot is returned when restore is requested without a snapshot.
var ErrNoSnapshot = errors.New("no network snapshot to restore")
