// Package config holds where claude-tickets keeps its state (per OS: see
// platform.DirsFor) and the settings it reads from the environment
// (CLAUDE_TICKETS_*) and from config.json in its config directory.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/platform"
)

func Home() string { return platform.Home() }

func ExpandHome(p string) string {
	if p == "~" {
		return Home()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(Home(), p[2:])
	}
	return p
}

func TildePath(p string) string {
	h := Home()
	if h == "" || !fsx.Within(p, h) {
		return p
	}
	rel, _ := filepath.Rel(h, p)
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

// Dir is the config directory (Linux: ~/.config/claude-tickets; see
// platform.DirsFor).
func Dir() string { return platform.Current().Config }

func File() string { return filepath.Join(Dir(), "config.json") }

// CacheDir is the cache directory ($CLAUDE_TICKETS_CACHE; Linux:
// ~/.cache/claude-tickets).
func CacheDir() string { return platform.Current().Cache }

// StateDir is where logs go ($CLAUDE_TICKETS_STATE_DIR; Linux:
// ~/.local/state).
func StateDir() string { return platform.Current().Logs }

func ObsidianRoot() string {
	if d := os.Getenv("OBSIDIAN_ROOT"); d != "" {
		return d
	}
	return filepath.Join(Home(), "Documents", "Obsidian")
}

// LaunchVault is set (CLAUDE_TICKETS_VAULT) for the sessions ct starts.
var LaunchVault = os.Getenv("CLAUDE_TICKETS_VAULT")

// DefaultVaultFile holds the default vault's name (or path). Before
// claude-tickets had its own config dir it lived in ~/.config/tickets, which
// is still read while the new file doesn't exist.
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

func LegacyDefaultVaultFile() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(Home(), ".config")
	}
	return filepath.Join(base, "tickets", "default-vault")
}

// Get returns "" when the key is unset or config.json is unreadable.
func Get(key string) string {
	m, _ := load()
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

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

// StateLog is the log file of a command, <state dir>/<name>.log, kept
// bounded: past 5 MB it's cut to its newest 1 MB.
func StateLog(name string) string {
	dir := StateDir()
	_ = os.MkdirAll(dir, 0o755)
	log := filepath.Join(dir, name+".log")
	if st, err := os.Stat(log); err == nil && st.Size() > 5<<20 {
		if f, err := os.Open(log); err == nil {
			tail := make([]byte, 1<<20)
			n, _ := f.ReadAt(tail, st.Size()-int64(len(tail)))
			f.Close()
			if os.WriteFile(log+".tmp", tail[:n], 0o600) == nil {
				_ = os.Rename(log+".tmp", log)
			}
		}
	}
	return log
}

// ReadJSON decodes a JSON file into a generic value, numbers kept as
// written (json.Number).
func ReadJSON(file string) (any, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s isn't valid JSON: %w", file, err)
	}
	return v, nil
}

func WriteJSON(file string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := os.WriteFile(file+".tmp", buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}
