//go:build linux

package browser

import (
	"fmt"
	"os"
	"os/exec"
)

// OpenApp tries to open url in an app-like browser window.
func OpenApp(url string) error {
	candidates := [][]string{
		{"google-chrome", "--app=" + url},
		{"chromium", "--app=" + url},
		{"chromium-browser", "--app=" + url},
		{"brave-browser", "--app=" + url},
		{"microsoft-edge", "--app=" + url},
		{"xdg-open", url},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	return fmt.Errorf("no browser found to open %s", url)
}
