// Command vpnctl is the unprivileged CLI client for vpnctld.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/daemonize"
	"github.com/saeedshamc/vpnctl/internal/install"
	"github.com/saeedshamc/vpnctl/internal/ipc"
	"github.com/saeedshamc/vpnctl/internal/paths"
	"github.com/saeedshamc/vpnctl/internal/platform"
	"github.com/saeedshamc/vpnctl/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 1
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "version", "--version", "-V":
		fmt.Println(version.String())
		return 0
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return 0
	case "config":
		return cmdConfig(rest)
	}

	if !platform.Supported() {
		fmt.Fprintln(os.Stderr, platform.UnsupportedReason())
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "up":
		return cmdUp(ctx, rest)
	case "down":
		if len(rest) != 0 {
			fmt.Fprintln(os.Stderr, "usage: vpnctl down")
			return ipc.CodeBadRequest
		}
		return cmdDown(ctx)
	case "status":
		return cmdStatus(ctx, rest)
	case "test":
		return cmdTest(ctx, rest)
	case "logs":
		return cmdLogs(rest)
	case "service":
		return cmdService(rest)
	case "autostart":
		return cmdAutostart(rest)
	case "uninstall":
		return cmdUninstall(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage(os.Stderr)
		return 1
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `vpnctl — use the VPN on your Android phone from Linux over USB

Usage:
  vpnctl up [--phone-port N] [--local-port N] [--proxy socks5|http]
  vpnctl down
  vpnctl status [--json]
  vpnctl test
  vpnctl config get|set <key> [value]
  vpnctl logs [-f]
  vpnctl service enable|disable
  vpnctl autostart enable|disable
  vpnctl version

Exit codes: 0 ok · 2 phone/adb · 3 proxy dead · 4 network · 5 permission
`)
}

func client() *ipc.Client {
	return &ipc.Client{Path: paths.SocketPath(), Timeout: 90 * time.Second}
}

func ensure(ctx context.Context) error {
	return daemonize.EnsureRunning(ctx, paths.SocketPath())
}

func call(ctx context.Context, method string, params map[string]any) (ipc.Response, int) {
	if err := ensure(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "helper: %v\n", err)
		return ipc.Response{}, ipc.CodePermission
	}
	resp, err := client().Call(method, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rpc: %v\n", err)
		return ipc.Response{}, ipc.CodeInternal
	}
	if !resp.OK {
		code := ipc.CodeInternal
		msg := "request failed"
		if resp.Error != nil {
			code = resp.Error.Code
			msg = resp.Error.Message
		}
		fmt.Fprintln(os.Stderr, msg)
		return resp, code
	}
	return resp, 0
}

func cmdUp(ctx context.Context, args []string) int {
	params, err := parseUpFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ipc.CodeBadRequest
	}
	resp, code := call(ctx, ipc.MethodUp, params)
	if code != 0 {
		return code
	}
	printStatus(resp.Result, false)
	return 0
}

func cmdDown(ctx context.Context) int {
	resp, code := call(ctx, ipc.MethodDown, nil)
	if code != 0 {
		return code
	}
	printStatus(resp.Result, false)
	return 0
}

func cmdStatus(ctx context.Context, args []string) int {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			fmt.Fprintf(os.Stderr, "unknown status option: %s\n", a)
			return ipc.CodeBadRequest
		}
	}
	resp, code := call(ctx, ipc.MethodStatus, nil)
	if code != 0 {
		return code
	}
	printStatus(resp.Result, asJSON)
	return 0
}

func cmdTest(ctx context.Context, args []string) int {
	params, err := parseUpFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ipc.CodeBadRequest
	}
	resp, code := call(ctx, ipc.MethodTest, params)
	// Even on failure the daemon may return a TestResult payload.
	if resp.Result != nil {
		printTest(resp.Result)
	}
	return code
}

func cmdConfig(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: vpnctl config get|set <key> [value]")
		return ipc.CodeBadRequest
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: vpnctl config get <key>")
			return ipc.CodeBadRequest
		}
		v, err := cfg.Get(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return ipc.CodeBadRequest
		}
		fmt.Println(v)
		return 0
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: vpnctl config set <key> <value>")
			return ipc.CodeBadRequest
		}
		if err := cfg.Set(args[1], strings.Join(args[2:], " ")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return ipc.CodeBadRequest
		}
		if err := config.SaveUser(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("ok")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: vpnctl config get|set <key> [value]")
		return ipc.CodeBadRequest
	}
}

func cmdLogs(args []string) int {
	follow := false
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			follow = true
		} else {
			fmt.Fprintf(os.Stderr, "unknown logs option: %s\n", a)
			return ipc.CodeBadRequest
		}
	}
	path := paths.LogPath()
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no log file yet at %s (%v)\n", path, err)
		return 1
	}
	defer f.Close()
	if !follow {
		_, err := io.Copy(os.Stdout, f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	_, _ = io.Copy(os.Stdout, f)
	for {
		n, err := io.Copy(os.Stdout, f)
		if err != nil && err != io.EOF {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if n == 0 {
			time.Sleep(300 * time.Millisecond)
		}
	}
}

func cmdService(args []string) int {
	if len(args) != 1 || (args[0] != "enable" && args[0] != "disable") {
		fmt.Fprintln(os.Stderr, "usage: vpnctl service enable|disable")
		return ipc.CodeBadRequest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var err error
	if args[0] == "enable" {
		err = install.ServiceEnable(ctx)
	} else {
		err = install.ServiceDisable(ctx)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "service %s: %v\n", args[0], err)
		return ipc.CodePermission
	}
	fmt.Printf("service %s\n", args[0])
	return 0
}

func cmdAutostart(args []string) int {
	if len(args) != 1 || (args[0] != "enable" && args[0] != "disable") {
		fmt.Fprintln(os.Stderr, "usage: vpnctl autostart enable|disable")
		return ipc.CodeBadRequest
	}
	var err error
	if args[0] == "enable" {
		err = install.AutostartEnable()
	} else {
		err = install.AutostartDisable()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "autostart %s: %v\n", args[0], err)
		return 1
	}
	fmt.Printf("autostart %s\n", args[0])
	return 0
}

func cmdUninstall(args []string) int {
	purge := false
	if len(args) == 1 && args[0] == "--purge" {
		purge = true
	} else if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: 		vpnctl uninstall [--purge]")
		return ipc.CodeBadRequest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := install.Uninstall(ctx, purge); err != nil {
		fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
		return 1
	}
	fmt.Println("uninstall complete")
	return 0
}

func parseUpFlags(args []string) (map[string]any, error) {
	out := map[string]any{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		need := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "--phone-port":
			v, err := need()
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("bad --phone-port")
			}
			out["phone_port"] = n
		case "--local-port":
			v, err := need()
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("bad --local-port")
			}
			out["local_port"] = n
		case "--proxy":
			v, err := need()
			if err != nil {
				return nil, err
			}
			out["proxy_type"] = v
		default:
			return nil, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return out, nil
}

func printStatus(raw any, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(raw)
		return
	}
	b, err := json.Marshal(raw)
	if err != nil {
		fmt.Printf("%v\n", raw)
		return
	}
	var s ipc.StatusResult
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Println(string(b))
		return
	}
	fmt.Printf("state:        %s\n", s.State)
	if s.PhoneState != "" {
		fmt.Printf("phone:        %s\n", s.PhoneState)
	}
	if s.ExitIP != "" {
		fmt.Printf("exit:         %s %s\n", s.ExitIP, s.ExitCountry)
	}
	if s.UptimeSec > 0 {
		fmt.Printf("uptime:       %ds\n", s.UptimeSec)
	}
	if s.LocalPort > 0 {
		fmt.Printf("ports:        local=%d phone=%d (%s)\n", s.LocalPort, s.PhonePort, s.ProxyType)
	}
	if s.LastError != "" {
		fmt.Printf("last_error:   %s\n", s.LastError)
	}
}

func printTest(raw any) {
	b, err := json.Marshal(raw)
	if err != nil {
		fmt.Printf("%v\n", raw)
		return
	}
	var t ipc.TestResult
	if err := json.Unmarshal(b, &t); err != nil {
		fmt.Println(string(b))
		return
	}
	status := "FAIL"
	if t.OK {
		status = "OK"
	}
	fmt.Printf("%s  phone=%s  latency=%dms\n", status, t.PhoneState, t.LatencyMS)
	if t.Message != "" {
		fmt.Println(t.Message)
	}
}
