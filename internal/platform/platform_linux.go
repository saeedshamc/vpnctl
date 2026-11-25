//go:build linux

package platform

// Supported reports whether the full vpnctl stack can run on this OS.
func Supported() bool { return true }

// UnsupportedReason is empty on Linux.
func UnsupportedReason() string { return "" }
