// Package config loads, validates and persists vpnctl settings.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/saeedshamc/vpnctl/internal/paths"
)

// ProxyType is the phone-side local proxy protocol.
type ProxyType string

const (
	ProxySOCKS5 ProxyType = "socks5"
	ProxyHTTP   ProxyType = "http"
)

// Config is the on-disk and in-memory settings document.
type Config struct {
	PhonePort     int       `toml:"phone_port"`
	LocalPort     int       `toml:"local_port"`
	ProxyType     ProxyType `toml:"proxy_type"`
	DNS           []string  `toml:"dns"`
	DNSOverTLS    bool      `toml:"dns_over_tls"`
	DisableIPv6   bool      `toml:"disable_ipv6"`
	TestURL       string    `toml:"test_url"`
	LogLevel      string    `toml:"log_level"`
	LaunchAtLogin bool      `toml:"launch_at_login"`
	AutoConnect   bool      `toml:"auto_connect"`
	HelperAtBoot  bool      `toml:"helper_at_boot"`
}

// Default returns the documented safe defaults.
func Default() Config {
	return Config{
		PhonePort:     10808,
		LocalPort:     10808,
		ProxyType:     ProxySOCKS5,
		DNS:           []string{"1.1.1.1", "8.8.8.8"},
		DNSOverTLS:    true,
		DisableIPv6:   true,
		TestURL:       "https://github.com",
		LogLevel:      "info",
		LaunchAtLogin: true,
		AutoConnect:   false,
		HelperAtBoot:  true,
	}
}

// Validate checks ports, proxy type, DNS and test URL.
func (c Config) Validate() error {
	var errs []error
	if c.PhonePort < 1 || c.PhonePort > 65535 {
		errs = append(errs, fmt.Errorf("phone_port out of range: %d", c.PhonePort))
	}
	if c.LocalPort < 1 || c.LocalPort > 65535 {
		errs = append(errs, fmt.Errorf("local_port out of range: %d", c.LocalPort))
	}
	switch c.ProxyType {
	case ProxySOCKS5, ProxyHTTP, "":
	default:
		errs = append(errs, fmt.Errorf("proxy_type must be socks5 or http, got %q", c.ProxyType))
	}
	if len(c.DNS) == 0 {
		errs = append(errs, errors.New("dns must contain at least one address"))
	}
	for _, d := range c.DNS {
		if net.ParseIP(strings.TrimSpace(d)) == nil {
			errs = append(errs, fmt.Errorf("invalid dns address: %q", d))
		}
	}
	if c.TestURL == "" {
		errs = append(errs, errors.New("test_url is required"))
	} else if u, err := url.Parse(c.TestURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("test_url must be http(s) URL, got %q", c.TestURL))
	}
	switch strings.ToLower(c.LogLevel) {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log_level must be debug|info|warn|error, got %q", c.LogLevel))
	}
	return errors.Join(errs...)
}

// Normalize fills empty optional fields with defaults.
func (c *Config) Normalize() {
	d := Default()
	if c.PhonePort == 0 {
		c.PhonePort = d.PhonePort
	}
	if c.LocalPort == 0 {
		c.LocalPort = d.LocalPort
	}
	if c.ProxyType == "" {
		c.ProxyType = d.ProxyType
	}
	if len(c.DNS) == 0 {
		c.DNS = append([]string(nil), d.DNS...)
	}
	if c.TestURL == "" {
		c.TestURL = d.TestURL
	}
	if c.LogLevel == "" {
		c.LogLevel = d.LogLevel
	}
}

// LoadFile reads a TOML config from path. Missing file returns Default().
func LoadFile(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

// Load merges system then user config. User values win when the user file exists.
// Missing files are fine; at least Default() is returned.
func Load() (Config, error) {
	cfg := Default()

	if _, err := os.Stat(paths.SystemConfig); err == nil {
		sys, err := LoadFile(paths.SystemConfig)
		if err != nil {
			return cfg, err
		}
		cfg = sys
	}

	userPath, err := paths.UserConfigFile()
	if err != nil {
		return cfg, nil
	}
	if _, err := os.Stat(userPath); err == nil {
		user, err := LoadFile(userPath)
		if err != nil {
			return cfg, err
		}
		cfg = user
	}
	return cfg, nil
}

// SaveUser writes cfg to ~/.config/vpnctl/config.toml.
func SaveUser(cfg Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	dir, err := paths.UserConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return SaveTo(f, cfg)
}

// Get returns a string representation of a known key.
func (c Config) Get(key string) (string, error) {
	switch strings.ToLower(key) {
	case "phone_port":
		return fmt.Sprintf("%d", c.PhonePort), nil
	case "local_port":
		return fmt.Sprintf("%d", c.LocalPort), nil
	case "proxy_type":
		return string(c.ProxyType), nil
	case "dns":
		return strings.Join(c.DNS, ", "), nil
	case "dns_over_tls":
		return fmt.Sprintf("%t", c.DNSOverTLS), nil
	case "disable_ipv6":
		return fmt.Sprintf("%t", c.DisableIPv6), nil
	case "test_url":
		return c.TestURL, nil
	case "log_level":
		return c.LogLevel, nil
	case "launch_at_login":
		return fmt.Sprintf("%t", c.LaunchAtLogin), nil
	case "auto_connect":
		return fmt.Sprintf("%t", c.AutoConnect), nil
	case "helper_at_boot":
		return fmt.Sprintf("%t", c.HelperAtBoot), nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// Set mutates a known key from a string value.
func (c *Config) Set(key, value string) error {
	switch strings.ToLower(key) {
	case "phone_port":
		n, err := parsePort(value)
		if err != nil {
			return err
		}
		c.PhonePort = n
	case "local_port":
		n, err := parsePort(value)
		if err != nil {
			return err
		}
		c.LocalPort = n
	case "proxy_type":
		pt := ProxyType(strings.ToLower(value))
		if pt != ProxySOCKS5 && pt != ProxyHTTP {
			return fmt.Errorf("proxy_type must be socks5 or http")
		}
		c.ProxyType = pt
	case "dns":
		parts := splitCSV(value)
		if len(parts) == 0 {
			return errors.New("dns must not be empty")
		}
		for _, p := range parts {
			if net.ParseIP(p) == nil {
				return fmt.Errorf("invalid dns address: %q", p)
			}
		}
		c.DNS = parts
	case "dns_over_tls":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.DNSOverTLS = b
	case "disable_ipv6":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.DisableIPv6 = b
	case "test_url":
		c.TestURL = value
	case "log_level":
		c.LogLevel = strings.ToLower(value)
	case "launch_at_login":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.LaunchAtLogin = b
	case "auto_connect":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.AutoConnect = b
	case "helper_at_boot":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.HelperAtBoot = b
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return c.Validate()
}

func parsePort(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid port: %q", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port out of range: %d", n)
	}
	return n, nil
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %q", s)
	}
}

func splitCSV(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
