package ipc

import "testing"

func TestUpParamsFromMap(t *testing.T) {
	p := UpParamsFromMap(map[string]any{
		"phone_port": float64(10809),
		"local_port": "18080",
		"proxy_type": "http",
	})
	if p.PhonePort != 10809 || p.LocalPort != 18080 || p.ProxyType != "http" {
		t.Fatalf("%+v", p)
	}
}
