package singleinstance

import "errors"

// ErrAlreadyRunning means another vpnctl-gui holds the lock.
var ErrAlreadyRunning = errors.New("another vpnctl-gui instance is already running")
