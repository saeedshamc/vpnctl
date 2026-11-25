// Package install manages systemd helper-at-boot and XDG autostart toggles.
package install

import (
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotSupported is returned on non-Linux builds.
var ErrNotSupported = fmt.Errorf("install toggles are only supported on linux")

// AutostartPath returns ~/.config/autostart/vpnctl.desktop.
func AutostartPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "autostart", AutostartName), nil
}
