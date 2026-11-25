// Command vpnctl-installer is a self-contained graphical-first installer entrypoint.
// Phase 6: for systems without .deb — copies binaries and opens the first-run wizard.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/saeedshamc/vpnctl/internal/platform"
	"github.com/saeedshamc/vpnctl/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	showVer := flag.Bool("version", false, "print version")
	prefix := flag.String("prefix", "/usr", "install prefix")
	flag.Parse()
	if *showVer {
		fmt.Println(version.String())
		return 0
	}
	if runtime.GOOS != "linux" || !platform.Supported() {
		fmt.Fprintln(os.Stderr, platform.UnsupportedReason())
		return 1
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srcDir := filepath.Dir(exe)
	// Prefer sibling binaries next to the installer (portable layout).
	bins := []struct{ src, dst string }{
		{"vpnctl", filepath.Join(*prefix, "bin", "vpnctl")},
		{"vpnctl-gui", filepath.Join(*prefix, "bin", "vpnctl-gui")},
		{"vpnctld", filepath.Join(*prefix, "lib", "vpnctl", "vpnctld")},
		{"adb", filepath.Join(*prefix, "lib", "vpnctl", "adb")},
		{"tun2socks", filepath.Join(*prefix, "lib", "vpnctl", "tun2socks")},
	}
	for _, b := range bins {
		from := filepath.Join(srcDir, b.src)
		if _, err := os.Stat(from); err != nil {
			from = filepath.Join(srcDir, "bundle", b.src)
		}
		if _, err := os.Stat(from); err != nil {
			fmt.Fprintf(os.Stderr, "missing %s (place binaries next to the installer)\n", b.src)
			return 1
		}
		if err := installFile(from, b.dst, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "install %s: %v (try with sudo/pkexec)\n", b.dst, err)
			return 1
		}
		fmt.Println("installed", b.dst)
	}

	// Launch GUI wizard after install.
	gui := filepath.Join(*prefix, "bin", "vpnctl-gui")
	cmd := exec.Command(gui, "--wizard")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Start()
	fmt.Println("vpnctl installed. Finish setup in the window that opened.")
	return 0
}

func installFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
