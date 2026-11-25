// Package paths centralises filesystem locations used by vpnctl.
package paths

import (
	"os"
	"path/filepath"
)

const (
	AppName      = "vpnctl"
	RuntimeDir   = "/run/vpnctl"
	SocketName   = "vpnctl.sock"
	StateName    = "state.json"
	SystemConfig = "/etc/vpnctl/config.toml"
	LibDir       = "/usr/lib/vpnctl"
)

// SocketPath is the unix socket vpnctld listens on.
func SocketPath() string {
	return filepath.Join(RuntimeDir, SocketName)
}

// StatePath is the crash-recovery snapshot written by vpnctld.
func StatePath() string {
	return filepath.Join(RuntimeDir, StateName)
}

// UserConfigDir returns ~/.config/vpnctl (or $XDG_CONFIG_HOME/vpnctl).
func UserConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// UserConfigFile returns ~/.config/vpnctl/config.toml.
func UserConfigFile() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// EnsureRuntimeDir creates /run/vpnctl with mode 0755.
func EnsureRuntimeDir() error {
	return os.MkdirAll(RuntimeDir, 0o755)
}

// ResolveBinary looks for name in LibDir, then next to the running executable,
// then in ./bundle (dev checkout), then on PATH.
func ResolveBinary(name string) (string, error) {
	candidates := []string{
		filepath.Join(LibDir, name),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
		// When running from a build/ tree, also try ../bundle.
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "bundle", name))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "bundle", name))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	if p, err := lookPath(name); err == nil {
		return p, nil
	}
	return "", &MissingBinaryError{Name: name}
}

// MissingBinaryError is returned when a bundled helper cannot be found.
type MissingBinaryError struct {
	Name string
}

func (e *MissingBinaryError) Error() string {
	return "bundled binary not found: " + e.Name
}
