//go:build !linux

package singleinstance

import "os"

// Lock is a no-op outside Linux.
type Lock struct{}

func Acquire(name string) (*Lock, error) {
	_ = name
	_ = os.TempDir()
	return &Lock{}, nil
}

func (l *Lock) Close() error { return nil }
