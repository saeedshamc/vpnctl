//go:build !linux

package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

func OpenApp(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return fmt.Errorf("unsupported OS")
	}
	return cmd.Start()
}
