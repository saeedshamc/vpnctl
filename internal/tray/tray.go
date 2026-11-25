// Package tray implements a StatusNotifierItem tray icon (Linux desktops).
package tray

import "fmt"

// State is the visual connection state.
type State int

const (
	StateOff State = iota
	StateConnecting
	StateOn
	StateError
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateOn:
		return "on"
	case StateError:
		return "error"
	default:
		return "off"
	}
}

// Callbacks invoked from the tray menu / activation.
type Callbacks struct {
	OnConnect    func()
	OnDisconnect func()
	OnOpen       func()
	OnSettings   func()
	OnQuit       func()
}

// ErrNotSupported is returned when no tray backend exists.
var ErrNotSupported = fmt.Errorf("tray not supported on this platform")
