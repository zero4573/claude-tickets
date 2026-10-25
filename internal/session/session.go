// Package session prepares the Claude Code sessions the commands start:
// the claude command line (plugin, directories), the workspace's
// .claude/settings.json, trust in Claude's own config, and the code graph's
// MCP config. Sessions are plain Claude Code: only standard flags, and
// anything environment-specific comes from CLAUDE_TICKETS_* variables.
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/lock"
)

// PluginDir is the Claude Code plugin shipped with ct (skills, wave
// agents, hooks): $CLAUDE_TICKETS_PLUGIN (the Nix package sets it), else
// config.json's pluginDir, else the copy embedded in ct, unpacked to the
// cache.
func PluginDir() (string, error) {
	d := os.Getenv("CLAUDE_TICKETS_PLUGIN")
	// (config.json's, from an older install.sh, only while it's still there)
	if c := config.Get("pluginDir"); d == "" && c != "" && fsx.IsDir(c) {
		d = c
	}
	if d == "" {
		return assets.Materialize("plugin")
	}
	if _, err := os.Stat(filepath.Join(d, ".claude-plugin", "plugin.json")); err != nil {
		return "", fmt.Errorf("no claude-tickets plugin at %s (set CLAUDE_TICKETS_PLUGIN, or reinstall)", d)
	}
	return d, nil
}

// Command is the claude command a session runs ($CLAUDE_TICKETS_CLAUDE,
// split on spaces, else claude), with the plugin and an --add-dir for each
// of dirs that exists.
func Command(dirs ...string) ([]string, error) {
	plugin, err := PluginDir()
	if err != nil {
		return nil, err
	}
	argv := strings.Fields(os.Getenv("CLAUDE_TICKETS_CLAUDE"))
	if len(argv) == 0 {
		argv = []string{"claude"}
	}
	argv = append(argv, "--plugin-dir", plugin)
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			argv = append(argv, "--add-dir="+d)
		}
	}
	return argv, nil
}

// WriteSettings writes <dir>/.claude/settings.json, which Claude reads as
// the project settings:
//   - what the ticket system needs: the main clones (projectsRoot) and the
//     cache are never edited (// marks an absolute path in a rule), plus
//     extraDeny
//   - the workspace's own Go caches (GoEnv), so a session (in its
//     container) fetches and builds Go modules there
//   - merged with $CLAUDE_TICKETS_SESSION_SETTINGS, the user's own rules
//     (objects merge, arrays are joined)
//
// It also writes <dir>/.ignore (WriteIgnore).
func WriteSettings(dir, projectsRoot string, extraDeny ...string) error {
	var deny []any
	for _, p := range []string{projectsRoot, config.CacheDir()} {
		for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
			deny = append(deny, fmt.Sprintf("%s(/%s/**)", tool, p))
		}
	}
	for _, r := range extraDeny {
		deny = append(deny, r)
	}
	env := map[string]any{}
	for k, v := range GoEnv(dir) {
		env[k] = v
	}
	var settings any = map[string]any{"permissions": map[string]any{"deny": deny}, "env": env}
	if f := os.Getenv("CLAUDE_TICKETS_SESSION_SETTINGS"); f != "" {
		var extra any
		data, err := os.ReadFile(f)
		if err == nil {
			err = json.Unmarshal(data, &extra)
		}
		if err != nil {
			return fmt.Errorf("CLAUDE_TICKETS_SESSION_SETTINGS (%s) isn't a readable JSON file", f)
		}
		settings = merge(settings, extra)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return err
	}
	if err := WriteIgnore(dir); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, ".claude", "settings.json"), settings)
}

// CacheDir is a workspace's cache folder, <dir>/.cache: Go's module and
// build caches (GoEnv). graphify never scans a .cache folder, and .ignore
// (WriteIgnore) keeps it out of fff and rg (Claude's Grep) searches unless
// a search names it.
func CacheDir(dir string) string { return filepath.Join(dir, ".cache") }

// GoEnv is the Go environment of a workspace's sessions: GOPATH, its
// module cache and the build cache under <dir>/.cache/go, so every ticket
// container fetches its own modules into a folder it can write, and
// ct ws rm removes them with the workspace.
func GoEnv(dir string) map[string]string {
	goDir := filepath.Join(CacheDir(dir), "go")
	return map[string]string{
		"GOPATH":     goDir,
		"GOMODCACHE": filepath.Join(goDir, "pkg", "mod"),
		"GOCACHE":    filepath.Join(goDir, "build"),
	}
}

// ignoreFile is <dir>/.ignore, read by fff and rg (the ignore-file format
// both share, honoured outside a git repo too).
const ignoreFile = "# Written by ct; regenerated on every start, so don't edit it.\n" +
	"# fff and rg (Claude's Grep) skip these unless a search names them.\n" +
	"/.cache/\n"

// WriteIgnore writes <dir>/.ignore.
func WriteIgnore(dir string) error {
	return os.WriteFile(filepath.Join(dir, ".ignore"), []byte(ignoreFile), 0o644)
}

// merge is a deep merge of b into a: objects key by key, arrays joined
// (sorted, without duplicates), else b unless it's null.
func merge(a, b any) any {
	switch av := a.(type) {
	case map[string]any:
		if bv, ok := b.(map[string]any); ok {
			out := map[string]any{}
			for k, v := range av {
				out[k] = v
			}
			for k, v := range bv {
				out[k] = merge(av[k], v)
			}
			return out
		}
	case []any:
		if bv, ok := b.([]any); ok {
			return union(av, bv)
		}
	}
	if b == nil {
		return a
	}
	return b
}

func union(a, b []any) []any {
	seen := map[string]bool{}
	type item struct {
		key string
		v   any
	}
	var items []item
	for _, v := range append(append([]any{}, a...), b...) {
		k, _ := json.Marshal(v)
		if !seen[string(k)] {
			seen[string(k)] = true
			items = append(items, item{string(k), v})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it.v
	}
	return out
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return buf.Bytes(), err
}

func writeJSON(file string, v any) error {
	data, err := marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

// ClaudeConfig is Claude Code's own config: $CLAUDE_CONFIG_DIR/.claude.json,
// else ~/.claude.json.
func ClaudeConfig() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = config.Home()
	}
	return filepath.Join(dir, ".claude.json")
}

// Trust marks a workspace these tools created as trusted in Claude Code
// (projects[<dir>].hasTrustDialogAccepted), so a session started there
// gets straight to work instead of asking whether to trust the folder.
func Trust(dir string) error {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	file := ClaudeConfig()
	if trusted(file, dir) {
		return nil
	}
	// The lock file stays: removed, a writer waiting on it and a new one
	// creating it again would both hold "the lock"
	return lock.With(file+".tickets-lock", func() error {
		cfg := map[string]any{}
		if data, err := os.ReadFile(file); err == nil && len(bytes.TrimSpace(data)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber() // keep numbers exactly as written
			if err := dec.Decode(&cfg); err != nil {
				return fmt.Errorf("%s isn't valid JSON: %w", file, err)
			}
		}
		projects, _ := cfg["projects"].(map[string]any)
		if projects == nil {
			projects = map[string]any{}
			cfg["projects"] = projects
		}
		p, _ := projects[dir].(map[string]any)
		if p == nil {
			p = map[string]any{}
			projects[dir] = p
		}
		p["hasTrustDialogAccepted"] = true
		data, err := marshal(cfg)
		if err != nil {
			return err
		}
		// In place (same file, not a rename): running sandboxes may
		// bind-mount this very file
		return os.WriteFile(file, data, 0o600)
	})
}

func trusted(file, dir string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	return json.Unmarshal(data, &cfg) == nil && cfg.Projects[dir].HasTrustDialogAccepted
}

// WriteGraphMCP writes <dir>/.claude/graph-mcp.json, the MCP config of the
// workspace's code graph (ct graph mcp <dir>, as the graphify server), and
// returns its path. A ct on PATH is named bare, not by the path LookPath
// found: the session may run where that path doesn't exist (a container
// with its own PATH), and resolves ct on its own PATH instead.
func WriteGraphMCP(dir, vault string) (string, error) {
	ct := "ct"
	if _, err := exec.LookPath(ct); err != nil {
		if ct, err = os.Executable(); err != nil {
			return "", err
		}
	}
	file := filepath.Join(dir, ".claude", "graph-mcp.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	return file, writeJSON(file, map[string]any{"mcpServers": map[string]any{"graphify": map[string]any{
		"type": "stdio", "command": ct, "args": []string{"graph", "mcp", dir},
		"env": map[string]string{"CLAUDE_TICKETS_VAULT": vault},
	}}})
}
