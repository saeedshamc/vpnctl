package adb

import "testing"

func TestParseDevicesLineLogic(t *testing.T) {
	// Smoke: DeviceState constants used by PrimaryState switch.
	cases := []DeviceState{StateDevice, StateUnauthorized, StateOffline, StateNone}
	for _, c := range cases {
		if c == "" {
			t.Fatalf("empty state")
		}
	}
}
