//go:build linux

package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

func (it *Item) exportMenu() error {
	table := map[string]interface{}{
		"GetLayout":          it.menuGetLayout,
		"GetGroupProperties": it.menuGetGroupProperties,
		"GetProperty":        it.menuGetProperty,
		"Event":              it.menuEvent,
		"EventGroup":         it.menuEventGroup,
		"AboutToShow":        it.menuAboutToShow,
		"AboutToShowGroup":   it.menuAboutToShowGroup,
	}
	if err := it.conn.ExportMethodTable(table, menuPath, menuIFACE); err != nil {
		return err
	}
	propTable := map[string]interface{}{
		"Get":    it.menuPropGet,
		"GetAll": it.menuPropGetAll,
	}
	if err := it.conn.ExportMethodTable(propTable, menuPath, "org.freedesktop.DBus.Properties"); err != nil {
		return err
	}
	node := introspect.Node{
		Name: menuPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: menuIFACE},
		},
	}
	return it.conn.Export(introspect.NewIntrospectable(&node), menuPath, "org.freedesktop.DBus.Introspectable")
}

func (it *Item) menuPropGet(iface, name string) (dbus.Variant, *dbus.Error) {
	switch name {
	case "Version":
		return dbus.MakeVariant(uint32(3)), nil
	case "TextDirection":
		return dbus.MakeVariant("ltr"), nil
	case "Status":
		return dbus.MakeVariant("normal"), nil
	case "IconThemePath":
		return dbus.MakeVariant([]string{}), nil
	default:
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []interface{}{name})
	}
}

func (it *Item) menuPropGetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{
		"Version":       dbus.MakeVariant(uint32(3)),
		"TextDirection": dbus.MakeVariant("ltr"),
		"Status":        dbus.MakeVariant("normal"),
		"IconThemePath": dbus.MakeVariant([]string{}),
	}, nil
}

// layoutRoot builds dbusmenu root: (id, props, childrenVariants)
func (it *Item) layoutRoot() (uint32, []interface{}) {
	it.mu.Lock()
	connected := it.connected
	it.mu.Unlock()

	var kids []interface{}
	if connected {
		kids = append(kids, menuLeaf(2, "Disconnect"))
	} else {
		kids = append(kids, menuLeaf(1, "Connect"))
	}
	kids = append(kids,
		menuSep(10),
		menuLeaf(3, "Open"),
		menuLeaf(4, "Settings"),
		menuSep(11),
		menuLeaf(5, "Quit"),
	)
	childVars := make([]dbus.Variant, 0, len(kids))
	for _, k := range kids {
		childVars = append(childVars, dbus.MakeVariant(k))
	}
	root := []interface{}{
		int32(0),
		map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		childVars,
	}
	return 1, root
}

func menuLeaf(id int32, label string) []interface{} {
	return []interface{}{
		id,
		map[string]dbus.Variant{
			"label":   dbus.MakeVariant(label),
			"enabled": dbus.MakeVariant(true),
		},
		[]dbus.Variant{},
	}
}

func menuSep(id int32) []interface{} {
	return []interface{}{
		id,
		map[string]dbus.Variant{"type": dbus.MakeVariant("separator")},
		[]dbus.Variant{},
	}
}

func (it *Item) menuGetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, []interface{}, *dbus.Error) {
	_, _, _ = parentID, recursionDepth, propertyNames
	rev, root := it.layoutRoot()
	return rev, root, nil
}

func (it *Item) menuGetGroupProperties(ids []int32, propertyNames []string) ([][]interface{}, *dbus.Error) {
	return nil, nil
}

func (it *Item) menuGetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant(""), nil
}

func (it *Item) menuEvent(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	go func() {
		switch id {
		case 1:
			if it.cbs.OnConnect != nil {
				it.cbs.OnConnect()
			}
		case 2:
			if it.cbs.OnDisconnect != nil {
				it.cbs.OnDisconnect()
			}
		case 3:
			if it.cbs.OnOpen != nil {
				it.cbs.OnOpen()
			}
		case 4:
			if it.cbs.OnSettings != nil {
				it.cbs.OnSettings()
			}
		case 5:
			if it.cbs.OnQuit != nil {
				it.cbs.OnQuit()
			}
		}
	}()
	return nil
}

func (it *Item) menuEventGroup(events [][]interface{}) ([]int32, *dbus.Error) {
	return nil, nil
}

func (it *Item) menuAboutToShow(id int32) (bool, *dbus.Error) {
	return true, nil
}

func (it *Item) menuAboutToShowGroup(ids []int32) (updatesNeeded, idErrors []int32, err *dbus.Error) {
	return ids, nil, nil
}

func (it *Item) emitMenuLayout() {
	rev, _ := it.layoutRoot()
	_ = it.conn.Emit(menuPath, menuIFACE+".LayoutUpdated", rev, int32(0))
}
