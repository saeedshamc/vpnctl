//go:build !linux

package tray

// Item is a no-op tray on non-Linux hosts.
type Item struct{}

// Start returns ErrNotSupported outside Linux.
func Start(cbs Callbacks) (*Item, error) {
	_ = cbs
	return nil, ErrNotSupported
}

func (it *Item) SetState(s State, detail string) {}
func (it *Item) Notify(summary, body string)    {}
func (it *Item) Close() error                   { return nil }
