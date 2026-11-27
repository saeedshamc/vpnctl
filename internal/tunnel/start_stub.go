//go:build !linux

package tunnel

import "context"

// Start is unavailable outside Linux.
func Start(ctx context.Context, opt Options) (*Handle, error) {
	_, _ = ctx, opt
	return nil, ErrNotSupported
}

func destroyDevice(ctx context.Context, name string) error {
	_, _ = ctx, name
	return nil
}

func StopProcess(int, string) error {
	return ErrNotSupported
}

func CleanupStaleDevice(context.Context, string) error {
	return ErrNotSupported
}
