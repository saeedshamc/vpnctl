//go:build linux

package install

import (
	"os"
	"path/filepath"
	"strings"
)

// AutostartEnable writes the XDG autostart entry for the tray.
func AutostartEnable() error {
	path, err := AutostartPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := customizeAutostart(autostartDesktop)
	return os.WriteFile(path, body, 0o644)
}

// AutostartDisable removes the XDG autostart entry.
func AutostartDisable() error {
	path, err := AutostartPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AutostartEnabled reports whether the autostart file exists and is not Hidden=true.
func AutostartEnabled() (bool, error) {
	path, err := AutostartPath()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	s := string(data)
	if strings.Contains(s, "Hidden=true") {
		return false, nil
	}
	return true, nil
}

func customizeAutostart(src []byte) []byte {
	body := string(src)
	// Prefer installed GUI; fall back to vpnctl status window later — for now keep gui path,
	// but if only CLI is present, point Exec at a no-op-safe tray placeholder note.
	if _, err := os.Stat(SystemBinGUI); err != nil {
		// Until phase 4 ships vpnctl-gui, autostart a tiny wrapper that just keeps helper ready.
		if _, err := os.Stat(SystemBinVpnctl); err == nil {
			body = strings.Replace(body, "Exec=/usr/bin/vpnctl-gui --tray-only", "Exec=/usr/bin/vpnctl status", 1)
			body = strings.Replace(body, "Name=vpnctl (tray)", "Name=vpnctl", 1)
		}
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return []byte(body)
}

// SyncAutostartFromConfig applies launch_at_login.
func SyncAutostartFromConfig(enabled bool) error {
	if enabled {
		return AutostartEnable()
	}
	return AutostartDisable()
}
