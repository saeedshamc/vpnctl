//go:build linux

package tray

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	sniIFACE   = "org.kde.StatusNotifierItem"
	sniPath    = "/StatusNotifierItem"
	watcherBus = "org.kde.StatusNotifierWatcher"
	menuPath   = "/MenuBar"
	menuIFACE  = "com.canonical.dbusmenu"
)

// Item is a live StatusNotifierItem.
type Item struct {
	mu        sync.Mutex
	conn      *dbus.Conn
	props     *prop.Properties
	cbs       Callbacks
	state     State
	connected bool
}

// Start registers a tray icon. Returns ErrNotSupported if no watcher exists.
func Start(cbs Callbacks) (*Item, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	it := &Item{conn: conn, cbs: cbs, state: StateOff}
	if err := it.export(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := it.register(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrNotSupported, err)
	}
	_ = os.Getpid() // keep identity stable in logs if needed later
	return it, nil
}

func (it *Item) export() error {
	icon, _ := IconPNG(StateOff)
	propsSpec := map[string]map[string]*prop.Prop{
		sniIFACE: {
			"Category":   {Value: "Network", Writable: false, Emit: prop.EmitTrue},
			"Id":         {Value: "vpnctl", Writable: false, Emit: prop.EmitTrue},
			"Title":      {Value: "vpnctl", Writable: false, Emit: prop.EmitTrue},
			"Status":     {Value: "Active", Writable: false, Emit: prop.EmitTrue},
			"WindowId":   {Value: uint32(0), Writable: false, Emit: prop.EmitTrue},
			"IconName":   {Value: "", Writable: false, Emit: prop.EmitTrue},
			"IconPixmap": {Value: pixmapFromPNG(icon), Writable: false, Emit: prop.EmitTrue},
			"ToolTip":    {Value: tooltip{"", nil, "vpnctl", "Disconnected"}, Writable: false, Emit: prop.EmitTrue},
			"ItemIsMenu": {Value: false, Writable: false, Emit: prop.EmitTrue},
			"Menu":       {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitTrue},
		},
	}
	props, err := prop.Export(it.conn, sniPath, propsSpec)
	if err != nil {
		return err
	}
	it.props = props

	table := map[string]interface{}{
		"ContextMenu":        it.contextMenu,
		"Activate":           it.activate,
		"SecondaryActivate":  it.activate,
		"Scroll":             func(delta int32, orientation string) {},
	}
	if err := it.conn.ExportMethodTable(table, sniPath, sniIFACE); err != nil {
		return err
	}
	if err := it.exportMenu(); err != nil {
		return err
	}
	node := introspect.Node{
		Name: sniPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: sniIFACE},
		},
	}
	return it.conn.Export(introspect.NewIntrospectable(&node), sniPath, "org.freedesktop.DBus.Introspectable")
}

func (it *Item) register() error {
	obj := it.conn.Object(watcherBus, "/StatusNotifierWatcher")
	return obj.Call(watcherBus+".RegisterStatusNotifierItem", 0, sniPath).Err
}

func (it *Item) activate(x, y int32) *dbus.Error {
	_, _ = x, y
	if it.cbs.OnOpen != nil {
		go it.cbs.OnOpen()
	}
	return nil
}

func (it *Item) contextMenu(x, y int32) *dbus.Error {
	_, _ = x, y
	return nil
}

// SetState updates icon and tooltip.
func (it *Item) SetState(s State, detail string) {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.state = s
	it.connected = s == StateOn
	icon, err := IconPNG(s)
	if err != nil {
		return
	}
	body := detail
	if body == "" {
		switch s {
		case StateOn:
			body = "Connected"
		case StateConnecting:
			body = "Connecting…"
		case StateError:
			body = "Error"
		default:
			body = "Disconnected"
		}
	}
	_ = it.props.Set(sniIFACE, "IconPixmap", dbus.MakeVariant(pixmapFromPNG(icon)))
	_ = it.props.Set(sniIFACE, "ToolTip", dbus.MakeVariant(tooltip{"", nil, "vpnctl", body}))
	_ = it.conn.Emit(sniPath, sniIFACE+".NewIcon")
	_ = it.conn.Emit(sniPath, sniIFACE+".NewToolTip")
	it.emitMenuLayout()
}

// Notify sends a desktop notification (best-effort).
func (it *Item) Notify(summary, body string) {
	obj := it.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	_ = obj.Call("org.freedesktop.Notifications.Notify", 0,
		"vpnctl", uint32(0), "", summary, body, []string{}, map[string]dbus.Variant{}, int32(4000),
	).Err
}

// Close releases the bus connection.
func (it *Item) Close() error {
	if it.conn != nil {
		return it.conn.Close()
	}
	return nil
}

type tooltip struct {
	IconName    string
	IconPix     []pixmap
	Title       string
	Description string
}

type pixmap struct {
	Width  int
	Height int
	Bytes  []byte
}

func pixmapFromPNG(png []byte) []pixmap {
	if len(png) == 0 {
		return nil
	}
	return []pixmap{{Width: 32, Height: 32, Bytes: png}}
}
