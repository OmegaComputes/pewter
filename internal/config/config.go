// Package config stores non-secret CLI settings. Tokens never go in this file.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// DefaultAPI is the public Pewter API base. Override with --api-url or the
// PEWTER_API_URL environment variable (https only; http is allowed for localhost).
const DefaultAPI = "https://api.omegacompute.com/api/v1"

// File is the on-disk, non-secret config.
type File struct {
	APIURL    string     `json:"api_url,omitempty"`
	Profile   string     `json:"profile,omitempty"`
	SignedAt  *time.Time `json:"signed_in_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Dir returns the config directory: PEWTER_CONFIG_DIR, else XDG, else ~/.config/pewter.
func Dir(getenv func(string) string) (string, error) {
	if d := getenv("PEWTER_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "pewter"), nil
	}
	h := getenv("HOME")
	if h == "" {
		return "", errors.New("cannot find a home directory; set PEWTER_CONFIG_DIR")
	}
	return filepath.Join(h, ".config", "pewter"), nil
}

// Load reads config.json; a missing file is an empty config.
func Load(dir string) (File, error) {
	var f File
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	return f, json.Unmarshal(b, &f)
}

// Save writes config.json with mode 0600 in a 0700 directory.
func Save(dir string, f File) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "config.json"))
}
