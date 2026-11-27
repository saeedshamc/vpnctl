// Command vpnctl-installer is a self-contained graphical-first installer entrypoint.
// Phase 6: for systems without .deb — copies binaries and opens the first-run wizard.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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
	installOnly := flag.Bool("install-only", false, "")
	flag.Parse()
	if *showVer {
		fmt.Println(version.String())
		return 0
	}
	if runtime.GOOS != "linux" || !platform.Supported() {
		fmt.Fprintln(os.Stderr, platform.UnsupportedReason())
		return 1
	}
	if filepath.Clean(*prefix) != "/usr" {
		fmt.Fprintln(os.Stderr, "the current installer layout requires --prefix /usr")
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
	sources := make([]string, len(bins))
	for i, b := range bins {
		from := filepath.Join(srcDir, b.src)
		if _, err := os.Stat(from); err != nil {
			from = filepath.Join(srcDir, "bundle", b.src)
		}
		info, err := os.Stat(from)
		if err != nil || !info.Mode().IsRegular() {
			fmt.Fprintf(os.Stderr, "missing %s (place binaries next to the installer)\n", b.src)
			return 1
		}
		sources[i] = from
	}
	if !*installOnly && os.Geteuid() != 0 {
		pkexec, err := exec.LookPath("pkexec")
		if err != nil {
			fmt.Fprintln(os.Stderr, "pkexec not found; run the installer as root")
			return 1
		}
		cmd := exec.Command(pkexec, exe, "--install-only", "--prefix=/usr")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "privileged installation failed: %v\n", err)
			return 1
		}
		return launchWizard(filepath.Join(*prefix, "bin", "vpnctl-gui"))
	}
	if *installOnly && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "--install-only must be run through pkexec")
		return 1
	}
	for i, b := range bins {
		if err := installFile(sources[i], b.dst, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "install %s: %v (try with sudo/pkexec)\n", b.dst, err)
			return 1
		}
		fmt.Println("installed", b.dst)
	}

	if *installOnly {
		fmt.Println("installation complete")
		return 0
	}

	// Launch GUI wizard after install.
	gui := filepath.Join(*prefix, "bin", "vpnctl-gui")
	return launchWizard(gui)
}

func launchWizard(gui string) int {
	cmd := exec.Command(gui, "--wizard")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if os.Geteuid() == 0 {
		invokingUID := os.Getenv("PKEXEC_UID")
		if invokingUID == "" {
			invokingUID = os.Getenv("SUDO_UID")
		}
		if invokingUID != "" {
			invokingUser, err := user.LookupId(invokingUID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vpnctl installed, but could not find the invoking user: %v\n", err)
				return 1
			}
			runuser, err := exec.LookPath("runuser")
			if err != nil {
				fmt.Fprintf(os.Stderr, "vpnctl installed, but runuser is unavailable: %v\n", err)
				return 1
			}
			cmd = exec.Command(runuser, "-u", invokingUser.Username, "--", gui, "--wizard")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "vpnctl installed, but could not open the setup window: %v\n", err)
		return 1
	}
	fmt.Println("vpnctl installed. Finish setup in the window that opened.")
	return 0
}

func installFile(src, dst string, mode os.FileMode) error {
	installer, err := exec.LookPath("install")
	if err != nil {
		return fmt.Errorf("install command not found: %w", err)
	}
	args := []string{"-D", "-m", fmt.Sprintf("%04o", mode.Perm()), src, dst}
	cmd := exec.Command(installer, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s (%w)", filepath.Base(installer), output, err)
	}
	return nil
}
