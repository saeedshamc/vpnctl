package netcfg

import "testing"

func TestParseDefaultRoute(t *testing.T) {
	r := parseDefaultRoute("default via 192.168.1.1 dev wlan0 proto dhcp metric 600")
	if r.Gateway != "192.168.1.1" || r.Dev != "wlan0" || r.Metric != 600 {
		t.Fatalf("unexpected parse: %+v", r)
	}
}
