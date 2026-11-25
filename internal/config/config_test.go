package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestValidateRejectsBadPort(t *testing.T) {
	cfg := Default()
	cfg.PhonePort = 70000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for bad phone_port")
	}
}

func TestValidateRejectsBadProxy(t *testing.T) {
	cfg := Default()
	cfg.ProxyType = "vmess"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for bad proxy_type")
	}
}

func TestValidateRejectsBadDNS(t *testing.T) {
	cfg := Default()
	cfg.DNS = []string{"not-an-ip"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for bad dns")
	}
}

func TestLoadFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := Default()
	cfg.LocalPort = 18080
	cfg.AutoConnect = true

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(f, cfg); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalPort != 18080 || !got.AutoConnect {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSetGet(t *testing.T) {
	cfg := Default()
	if err := cfg.Set("dns", "9.9.9.9, 1.0.0.1"); err != nil {
		t.Fatal(err)
	}
	v, err := cfg.Get("dns")
	if err != nil {
		t.Fatal(err)
	}
	if v != "9.9.9.9, 1.0.0.1" {
		t.Fatalf("got %q", v)
	}
	if err := cfg.Set("proxy_type", "http"); err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyType != ProxyHTTP {
		t.Fatalf("proxy_type=%s", cfg.ProxyType)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	got, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := Default()
	if got.PhonePort != want.PhonePort || got.TestURL != want.TestURL {
		t.Fatalf("unexpected: %+v", got)
	}
}
