//go:build linux

package install

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnsureSystemFiles installs the systemd unit and polkit policy if missing.
// Safe to call repeatedly. Requires privilege via pkexec when not root.
func EnsureSystemFiles(ctx context.Context) error {
	if _, err := os.Stat(UnitPath); err != nil {
		tmp := "/tmp/vpnctld.service"
		if err := os.WriteFile(tmp, unitFile, 0o644); err != nil {
			return err
		}
		if _, err := runPrivileged(ctx, "/usr/bin/install", "-D", "-m", "644", tmp, UnitPath); err != nil {
			// try without absolute install
			if _, err2 := runPrivileged(ctx, "install", "-D", "-m", "644", tmp, UnitPath); err2 != nil {
				return fmt.Errorf("install unit: %v / %v", err, err2)
			}
		}
		if _, err := runPrivileged(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if _, err := os.Stat(PolkitPath); err != nil {
		tmp := "/tmp/org.vpnctl.daemon.policy"
		if err := os.WriteFile(tmp, polkitPolicy, 0o644); err != nil {
			return err
		}
		if _, err := runPrivileged(ctx, "/usr/bin/install", "-D", "-m", "644", tmp, PolkitPath); err != nil {
			if _, err2 := runPrivileged(ctx, "install", "-D", "-m", "644", tmp, PolkitPath); err2 != nil {
				return fmt.Errorf("install polkit policy: %v / %v", err, err2)
			}
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
		// systemctl returns non-zero for disabled
		s := strings.TrimSpace(out)
		if s == "disabled" || s == "masked" || s == "static" || s == "" {
			return false, nil
		}
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "No such file") {
			return false, nil
		}
		return false, nil
	}
	return strings.TrimSpace(out) == "enabled", nil
}
