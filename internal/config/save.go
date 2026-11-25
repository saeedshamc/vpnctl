package config

import (
	"io"

	"github.com/BurntSushi/toml"
)

// SaveTo encodes cfg as TOML to w.
func SaveTo(w io.Writer, cfg Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	return toml.NewEncoder(w).Encode(cfg)
}
