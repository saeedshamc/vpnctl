// Package version holds build-time identity for all vpnctl binaries.
package version

// These may be overridden at link time:
//
//	go build -ldflags "-X github.com/saeedshamc/vpnctl/internal/version.Version=1.0.0"
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a short human-readable version line.
func String() string {
	return "vpnctl " + Version + " (" + Commit + " " + Date + ")"
}
