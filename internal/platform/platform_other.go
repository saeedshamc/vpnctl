//go:build !linux

package platform

import "runtime"

// Supported is false outside Linux (v1 non-goal: Windows/macOS).
func Supported() bool { return false }

// UnsupportedReason explains why this build cannot run the tunnel.
func UnsupportedReason() string {
	return "vpnctl v1 only supports Linux (current OS: " + runtime.GOOS + ")"
}
