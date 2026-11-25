package install

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAutostartPath(t *testing.T) {
	p, err := AutostartPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("autostart", AutostartName)) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestEmbeddedAssetsNonEmpty(t *testing.T) {
	if len(unitFile) == 0 || len(polkitPolicy) == 0 || len(autostartDesktop) == 0 {
		t.Fatal("embedded install assets must not be empty")
	}
	if !strings.Contains(string(unitFile), "vpnctld") {
		t.Fatal("unit file missing vpnctld")
	}
}
