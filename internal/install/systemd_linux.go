//go:build linux

package install

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnsureSystemFiles installs or updates the systemd unit and polkit policy.
// Safe to call repeatedly. Requires privilege via pkexec when not root.
func EnsureSystemFiles(ctx context.Context) error {
	unitChanged, err := installEmbeddedFile(ctx, unitFile, UnitPath)
	if err != nil {
		return fmt.Errorf("install unit: %w", err)
	}
	if _, err := installEmbeddedFile(ctx, polkitPolicy, PolkitPath); err != nil {
		return fmt.Errorf("install polkit policy: %w", err)
	}
	if unitChanged {
		if _, err := runPrivileged(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

// ServiceEnable enables and starts vpnctld.service.
func ServiceEnable(ctx context.Context) error {
	if err := EnsureSystemFiles(ctx); err != nil {
		return err
	}
	if _, err := runPrivileged(ctx, "systemctl", "enable", "--now", UnitName); err != nil {
		return err
	}
	return nil
}

// ServiceDisable stops and disables vpnctld.service.
func ServiceDisable(ctx context.Context) error {
	// Prefer disable even if unit is absent.
	if _, err := runPrivileged(ctx, "systemctl", "disable", "--now", UnitName); err != nil {
		// If unit missing, treat as already disabled.
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "No such file") {
			return nil
		}
		return err
	}
	return nil
}

// ServiceEnabled reports whether the unit is enabled.
func ServiceEnabled(ctx context.Context) (bool, error) {
	systemctl, err := look("systemctl")
	if err != nil {
		return false, err
	}
	out, err := run(ctx, systemctl, "is-enabled", UnitName)
	if err != nil {
		s := strings.TrimSpace(out)
		switch s {
		case "disabled", "masked", "static", "indirect", "generated", "transient", "not-found":
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) == "enabled", nil
}

func Uninstall(ctx context.Context, purge bool) error {
	dp, err := look("dpkg-query")
	if err != nil {
		return fmt.Errorf("dpkg-query not found; this is not a Debian package installation")
	}
	status, err := run(ctx, dp, "-W", "-f=${db:Status-Status}", "vpnctl")
	if err != nil || status != "installed" {
		return fmt.Errorf("vpnctl is not installed as a Debian package; remove a portable installation manually")
	}
	dpkg, err := look("dpkg")
	if err != nil {
		return fmt.Errorf("dpkg not found: %w", err)
	}
	action := "-r"
	if purge {
		action = "-P"
	}
	_, err = runPrivileged(ctx, dpkg, action, "vpnctl")
	return err
}

func installEmbeddedFile(ctx context.Context, data []byte, destination string) (bool, error) {
	if current, err := os.ReadFile(destination); err == nil && string(current) == string(data) {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	tmp, err := os.CreateTemp("", "vpnctl-install-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	install, err := look("install")
	if err != nil {
		return false, fmt.Errorf("install command not found: %w", err)
	}
	_, err = runPrivileged(ctx, install, "-D", "-m", "644", tmpPath, destination)
	return err == nil, err
}
