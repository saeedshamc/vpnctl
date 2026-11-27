package state

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	f := NewDisconnected()
	f.ConnState = StateConnected
	f.LocalPort = 10808
	f.Snapshot = &Snapshot{
		DefaultRoutes:      []RouteSnapshot{{Dst: "default", Gateway: "1.2.3.4", Dev: "wlan0", Family: "inet"}},
		IPv6WasOn:          true,
		ResolvConfCaptured: true,
		ResolvConfExisted:  true,
		ResolvConfOriginal: []byte("search test\n"),
		ResolvConfWritten:  []byte("nameserver 1.1.1.1\n"),
		ResolvConfMode:     0o640,
	}
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnState != StateConnected || got.LocalPort != 10808 {
		t.Fatalf("mismatch: %+v", got)
	}
	if !got.NeedsRecovery() {
		t.Fatal("expected NeedsRecovery")
	}
	if string(got.Snapshot.ResolvConfOriginal) != "search test\n" || got.Snapshot.ResolvConfMode != 0o640 {
		t.Fatalf("DNS snapshot did not survive round-trip: %+v", got.Snapshot)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnState != StateDisconnected {
		t.Fatalf("got %s", got.ConnState)
	}
}
