//go:build linux

package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock tries to acquire an exclusive flock for vpnctl-gui.
// If another instance holds it, unlock is a no-op on Close of that other process.
type Lock struct {
	f *os.File
}

// Acquire returns ErrAlreadyRunning when another GUI is alive.
func Acquire(name string) (*Lock, error) {
	dir := filepath.Join(os.TempDir(), "vpnctl")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w", ErrAlreadyRunning)
	}
	_, _ = f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
	return &Lock{f: f}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
