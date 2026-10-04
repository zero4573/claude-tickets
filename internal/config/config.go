// Package config holds where claude-tickets keeps its state and the
// settings it reads from the environment (CLAUDE_TICKETS_*) and from
// ~/.config/claude-tickets/config.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Home is the user's home directory ($HOME, else the OS's idea of it).
func Home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// ExpandHome turns a leading ~ into the home directory.
func ExpandHome(p string) string {
	if p == "~" {
		return Home()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(Home(), p[2:])
	}
	return p
}

// TildePath shows a path under the home directory as ~/...
func TildePath(p string) string {
	h := Home()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

func xdg(env, fallback string) string {
	if d := os.Getenv(env); d != "" {
		return d
	}
	return filepath.Join(Home(), fallback)
}

// Dir is ~/.config/claude-tickets (XDG_CONFIG_HOME respected).
func Dir() string { return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "claude-tickets") }

// File is the host-wide settings file, config.json.
func File() string { return filepath.Join(Dir(), "config.json") }

// CacheDir is $CLAUDE_TICKETS_CACHE, else ~/.cache/claude-tickets.
func CacheDir() string {
	if d := os.Getenv("CLAUDE_TICKETS_CACHE"); d != "" {
		return d
	}
	return filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), "claude-tickets")
}

// StateDir is where logs go: $XDG_STATE_HOME, else ~/.local/state.
func StateDir() string { return xdg("XDG_STATE_HOME", ".local/state") }

// ObsidianRoot is where the vaults live: $OBSIDIAN_ROOT, else
// ~/Documents/Obsidian.
func ObsidianRoot() string {
	if d := os.Getenv("OBSIDIAN_ROOT"); d != "" {
		return d
	}
	return filepath.Join(Home(), "Documents", "Obsidian")
}

// LaunchVault is the vault the command was started with
// (CLAUDE_TICKETS_VAULT, set for the sessions the commands start).
var LaunchVault = os.Getenv("CLAUDE_TICKETS_VAULT")

// DefaultVaultFile holds the default vault's name (or path). Before
// claude-tickets had its own config dir it lived in ~/.config/tickets.
func DefaultVaultFile() string {
	f := filepath.Join(Dir(), "default-vault")
	if _, err := os.Stat(f); err != nil {
		old := LegacyDefaultVaultFile()
		if st, err := os.Stat(old); err == nil && st.Size() > 0 {
			return old
		}
	}
	return f
}

// LegacyDefaultVaultFile is the pre-claude-tickets location.
func LegacyDefaultVaultFile() string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "tickets", "default-vault")
}

// Get reads a string key of config.json ("" when unset or unreadable).
func Get(key string) string {
	m, _ := load()
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// Set writes a string key of config.json, keeping the others.
func Set(key, value string) error {
	m, err := load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if m == nil {
		m = map[string]any{}
	}
	m[key] = value
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	tmp := File() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, File())
}

func load() (map[string]any, error) {
	data, err := os.ReadFile(File())
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	return m, json.Unmarshal(data, &m)
}
