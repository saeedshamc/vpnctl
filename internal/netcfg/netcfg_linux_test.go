//go:build linux

package netcfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/saeedshamc/vpnctl/internal/state"
)

func TestResolvConfRestoresExactFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	original := []byte("# generated\nsearch example.test\nnameserver 192.0.2.1\noptions timeout:2\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	snap := snapshotForResolvConf(t, path, resolvConfContents([]string{"1.1.1.1"}))
	if err := writeResolvConfBytes(path, snap.ResolvConfWritten, os.FileMode(snap.ResolvConfMode)); err != nil {
		t.Fatal(err)
	}
	if err := restoreResolvConf(path, snap); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored contents = %q, want %q", got, original)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %o, want 640", info.Mode().Perm())
	}
}

func TestRestoreResolvConfPreservesExternalChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 192.0.2.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := snapshotForResolvConf(t, path, resolvConfContents([]string{"1.1.1.1"}))
	if err := os.WriteFile(path, []byte("nameserver 192.0.2.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreResolvConf(path, snap); err == nil {
		t.Fatal("expected external DNS change to be preserved")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nameserver 192.0.2.53\n" {
		t.Fatalf("external change was overwritten: %q", got)
	}
}

func TestWriteResolvConfRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "resolv.conf")
	if err := os.WriteFile(target, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeResolvConf(path, []string{"1.1.1.1"}); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep me\n" {
		t.Fatalf("symlink target changed: %q", got)
	}
}

func snapshotForResolvConf(t *testing.T, path string, written []byte) *state.Snapshot {
	t.Helper()
	original, mode, existed, err := captureResolvConf(path)
	if err != nil {
		t.Fatal(err)
	}
	return &state.Snapshot{
		DNSMode:            "resolvconf",
		ResolvConfCaptured: true,
		ResolvConfExisted:  existed,
		ResolvConfOriginal: original,
		ResolvConfWritten:  written,
		ResolvConfMode:     uint32(mode.Perm()),
	}
}
