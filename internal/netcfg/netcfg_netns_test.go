//go:build linux

package netcfg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saeedshamc/vpnctl/internal/config"
	"github.com/saeedshamc/vpnctl/internal/state"
)

func TestNetworkNamespaceRoutes(t *testing.T) {
	if os.Getenv("VPNCTL_NETNS_TEST") != "1" {
		t.Skip("run scripts/test-netns.sh in an isolated network namespace")
	}
	ctx := context.Background()
	runIPTest(t, ctx, "ip", "link", "add", "vpnctl-upstream", "type", "dummy")
	runIPTest(t, ctx, "ip", "link", "set", "vpnctl-upstream", "up")
	runIPTest(t, ctx, "ip", "addr", "add", "192.0.2.1/24", "dev", "vpnctl-upstream")
	runIPTest(t, ctx, "ip", "route", "add", "default", "dev", "vpnctl-upstream", "metric", "600")
	runIPTest(t, ctx, "ip", "-6", "route", "add", "default", "dev", "vpnctl-upstream", "metric", "700")
	runIPTest(t, ctx, "ip", "link", "add", "tun0", "type", "dummy")
	runIPTest(t, ctx, "ip", "link", "set", "tun0", "up")

	fakeBin := t.TempDir()
	calls := filepath.Join(fakeBin, "resolvectl.calls")
	resolvectl := filepath.Join(fakeBin, "resolvectl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + calls + "\"\n"
	if err := os.WriteFile(resolvectl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+oldPath)

	before, err := listDefaultRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoute(before, "inet", "vpnctl-upstream", 600) || !hasRoute(before, "inet6", "vpnctl-upstream", 700) {
		t.Fatalf("initial routes were not captured: %+v", before)
	}
	snap := &state.Snapshot{DefaultRoutes: before, DNSMode: "resolved", IPv6WasOn: true}
	cfg := config.Default()
	cfg.DisableIPv6 = false
	if err := applyTunnel(ctx, "tun0", cfg, snap); err != nil {
		t.Fatal(err)
	}
	during, err := listDefaultRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoute(during, "inet", "tun0", 1) || !hasRoute(during, "inet6", "tun0", 1) {
		t.Fatalf("tunnel routes were not installed: %+v", during)
	}
	if err := restore(ctx, snap); err != nil {
		t.Fatal(err)
	}
	after, err := listDefaultRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoute(after, "inet", "vpnctl-upstream", 600) || !hasRoute(after, "inet6", "vpnctl-upstream", 700) {
		t.Fatalf("original routes were not restored: %+v", after)
	}
	if hasRoute(after, "inet", "tun0", 1) || hasRoute(after, "inet6", "tun0", 1) {
		t.Fatalf("vpnctl routes remain after restore: %+v", after)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "revert tun0") {
		t.Fatalf("resolved DNS was not reverted: %s", data)
	}
}

func runIPTest(t *testing.T, ctx context.Context, name string, args ...string) {
	t.Helper()
	if err := run(ctx, name, args...); err != nil {
		t.Fatal(err)
	}
}

func hasRoute(routes []state.RouteSnapshot, family, dev string, metric int) bool {
	for _, route := range routes {
		if route.Family == family && route.Dev == dev && route.Metric == metric {
			return true
		}
	}
	return false
}
