//go:build !linux

package install

import "context"

func EnsureSystemFiles(ctx context.Context) error {
	_ = ctx
	return ErrNotSupported
}

func ServiceEnable(ctx context.Context) error {
	_ = ctx
	return ErrNotSupported
}

func ServiceDisable(ctx context.Context) error {
	_ = ctx
	return ErrNotSupported
}

func ServiceEnabled(ctx context.Context) (bool, error) {
	_ = ctx
	return false, ErrNotSupported
}

func AutostartEnable() error  { return ErrNotSupported }
func AutostartDisable() error { return ErrNotSupported }
func AutostartEnabled() (bool, error) {
	return false, ErrNotSupported
}
func SyncAutostartFromConfig(enabled bool) error {
	_ = enabled
	return ErrNotSupported
}
