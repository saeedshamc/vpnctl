//go:build linux

package adb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveForwardIgnoresOnlyMissingListener(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{name: "missing forward", output: "error: listener 'tcp:10808' not found"},
		{name: "other error", output: "error: permission denied", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "adb")
			script := "#!/bin/sh\nprintf '%s\\n' '" + tt.output + "' >&2\nexit 1\n"
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			client := &Client{Bin: bin}
			err := client.RemoveForward(context.Background(), 10808)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RemoveForward() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
