// Package adb wraps the bundled Android Debug Bridge binary.
package adb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/saeedshamc/vpnctl/internal/paths"
)

// DeviceState is the adb connection state for a phone.
type DeviceState string

const (
	StateDevice       DeviceState = "device"
	StateUnauthorized DeviceState = "unauthorized"
	StateOffline      DeviceState = "offline"
	StateNone         DeviceState = "none"
	StateUnknown      DeviceState = "unknown"
)

// Client runs adb with an absolute path (never via shell).
type Client struct {
	Bin     string
	Timeout time.Duration
}

// New resolves the bundled (or PATH) adb binary.
func New() (*Client, error) {
	bin, err := paths.ResolveBinary("adb")
	if err != nil {
		return nil, err
	}
	return &Client{Bin: bin, Timeout: 15 * time.Second}, nil
}

// Run executes adb with fixed argv (no shell).
func (c *Client) Run(ctx context.Context, args ...string) (string, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = out
		}
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("adb %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

// Device is one entry from `adb devices`.
type Device struct {
	Serial string
	State  DeviceState
}

// Devices lists connected phones.
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	out, err := c.Run(ctx, "devices")
	if err != nil {
		return nil, err
	}
	var list []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		list = append(list, Device{
			Serial: fields[0],
			State:  DeviceState(fields[1]),
		})
	}
	return list, nil
}

// PrimaryState returns the state of the first useful device, or StateNone.
func (c *Client) PrimaryState(ctx context.Context) (DeviceState, string, error) {
	devs, err := c.Devices(ctx)
	if err != nil {
		return StateUnknown, "", err
	}
	if len(devs) == 0 {
		return StateNone, "", nil
	}
	// Prefer an authorised device.
	for _, d := range devs {
		if d.State == StateDevice {
			return d.State, d.Serial, nil
		}
	}
	d := devs[0]
	return d.State, d.Serial, nil
}

// RequireDevice ensures a phone is present and authorised.
func (c *Client) RequireDevice(ctx context.Context) (serial string, err error) {
	st, serial, err := c.PrimaryState(ctx)
	if err != nil {
		return "", err
	}
	switch st {
	case StateDevice:
		return serial, nil
	case StateUnauthorized:
		return serial, fmt.Errorf("phone unauthorized: unlock the phone and allow USB debugging")
	case StateOffline:
		return serial, fmt.Errorf("phone offline: replug the USB cable")
	case StateNone:
		return "", fmt.Errorf("no phone detected: plug in the phone with USB debugging enabled")
	default:
		return serial, fmt.Errorf("unexpected adb state %q", st)
	}
}

// Forward sets up `adb forward tcp:local tcp:remote`. Idempotent enough for our use:
// existing forwards to the same ports are fine; we remove+add to be sure.
func (c *Client) Forward(ctx context.Context, localPort, remotePort int) error {
	if localPort < 1 || localPort > 65535 || remotePort < 1 || remotePort > 65535 {
		return fmt.Errorf("invalid forward ports local=%d remote=%d", localPort, remotePort)
	}
	_, _ = c.Run(ctx, "forward", "--remove", fmt.Sprintf("tcp:%d", localPort))
	_, err := c.Run(ctx, "forward", fmt.Sprintf("tcp:%d", localPort), fmt.Sprintf("tcp:%d", remotePort))
	return err
}

// RemoveForward drops a local forward. Missing forward is not an error.
func (c *Client) RemoveForward(ctx context.Context, localPort int) error {
	if localPort < 1 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	_, err := c.Run(ctx, "forward", "--remove", fmt.Sprintf("tcp:%d", localPort))
	if err != nil && strings.Contains(err.Error(), "not found") {
		return nil
	}
	// adb may also print to stderr differently; ignore soft failures on cleanup.
	_ = err
	return nil
}
