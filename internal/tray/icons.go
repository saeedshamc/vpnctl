package tray

import (
	"embed"
	"fmt"
)

//go:embed icons/*.png
var iconFS embed.FS

// IconPNG returns the embedded tray icon for a state.
func IconPNG(s State) ([]byte, error) {
	name := fmt.Sprintf("icons/tray-%s.png", s.String())
	return iconFS.ReadFile(name)
}
