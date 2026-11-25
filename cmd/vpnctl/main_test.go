package main

import "testing"

func TestParseUpFlags(t *testing.T) {
	p, err := parseUpFlags([]string{"--phone-port", "10809", "--proxy", "socks5"})
	if err != nil {
		t.Fatal(err)
	}
	if p["phone_port"] != 10809 || p["proxy_type"] != "socks5" {
		t.Fatalf("%v", p)
	}
}

func TestParseUpFlagsUnknown(t *testing.T) {
	if _, err := parseUpFlags([]string{"--nope"}); err == nil {
		t.Fatal("expected error")
	}
}
