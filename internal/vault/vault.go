// Package vault picks the Obsidian vault the commands act on and reads
// where its tools work (<vault>/.workflow.json).
package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/claude-tickets/internal/config"
)

// List names every vault: a directory under the Obsidian root holding
// .obsidian/.
func List() []string {
	root := config.ObsidianRoot()
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if st, err := os.Stat(filepath.Join(root, e.Name(), ".obsidian")); err == nil && st.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// isVault reports whether dir holds .obsidian/.
func isVault(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".obsidian"))
	return err == nil && st.IsDir()
}

// Resolve turns a vault name (under the Obsidian root) or path into the
// vault's absolute path.
func Resolve(name string) (string, error) {
	if isVault(name) {
		return filepath.Abs(name)
	}
	p := filepath.Join(config.ObsidianRoot(), name)
	if isVault(p) {
		return p, nil
	}
	return "", fmt.Errorf("no vault named '%s' under %s", name, config.ObsidianRoot())
}

// workspaceVault is the vault recorded in the workspace.json of the ticket
// or kb workspace dir is in, if any.
func workspaceVault(dir string) string {
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, "workspace.json"))
		if err != nil {
			continue
		}
		var ws struct {
			Vault string `json:"vault"`
		}
		if json.Unmarshal(data, &ws) == nil && ws.Vault != "" {
			return ws.Vault
		}
	}
	return ""
}

// Default is the default vault: CLAUDE_TICKETS_VAULT (a session's vault,
// or the caller's when one command runs another), else the vault of the
// ticket or kb workspace the current directory is in, else the name saved
// by `ct vault default`. ok is false when none of them names a vault.
func Default() (path string, ok bool, err error) {
	name := config.LaunchVault
	if name == "" {
		if wd, e := os.Getwd(); e == nil {
			name = workspaceVault(wd)
		}
	}
	if name == "" {
		name = SavedDefault()
	}
	if name == "" {
		return "", false, nil
	}
	p, err := Resolve(name)
	if err != nil {
		return "", false, fmt.Errorf("default vault '%s' isn't a vault (ct vault default to change it), ignoring it", name)
	}
	return p, true, nil
}

// SavedDefault is the name or path saved in the default-vault file.
func SavedDefault() string {
	data, _ := os.ReadFile(config.DefaultVaultFile())
	return strings.TrimSpace(string(data))
}

// Current is the vault the commands act on: the default vault, else the
// only vault.
func Current() (string, error) {
	p, ok, err := Default()
	if ok {
		return p, nil
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ct: "+err.Error())
	}
	vaults := List()
	switch {
	case len(vaults) == 1:
		return filepath.Join(config.ObsidianRoot(), vaults[0]), nil
	case len(vaults) == 0:
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("no Obsidian vaults under %s (set one up with ct vault init)", config.ObsidianRoot())
	default:
		return "", fmt.Errorf("no default vault, and several under %s (%s): pick one with ct vault default <name>",
			config.ObsidianRoot(), strings.Join(vaults, " "))
	}
}

// SetDefault makes vault the default: its name when it lives under the
// Obsidian root, else its path. Returns what was saved.
func SetDefault(vault string) (string, error) {
	_ = os.Remove(config.LegacyDefaultVaultFile())
	saved := vault
	if filepath.Dir(vault) == filepath.Clean(config.ObsidianRoot()) {
		saved = filepath.Base(vault)
	}
	file := filepath.Join(config.Dir(), "default-vault")
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		return "", err
	}
	return saved, os.WriteFile(file, []byte(saved+"\n"), 0o644)
}

// UnsetDefault removes the saved default.
func UnsetDefault() error {
	_ = os.Remove(config.LegacyDefaultVaultFile())
	err := os.Remove(filepath.Join(config.Dir(), "default-vault"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Locations is where a vault's tools work.
type Locations struct {
	WorkRoot     string // ticket and kb workspaces (default ~/work/<vault>)
	ProjectsRoot string // main clones (default ~/Projects; vaults may share it)
	TmuxSession  string // always tickets-<vault>
}

// LocationsOf reads <vault>/.workflow.json, defaulting missing keys.
func LocationsOf(vault string) Locations {
	name := filepath.Base(vault)
	var wf struct {
		WorkRoot     string `json:"workRoot"`
		ProjectsRoot string `json:"projectsRoot"`
	}
	if data, err := os.ReadFile(filepath.Join(vault, ".workflow.json")); err == nil {
		_ = json.Unmarshal(data, &wf)
	}
	l := Locations{
		WorkRoot:     config.ExpandHome(wf.WorkRoot),
		ProjectsRoot: config.ExpandHome(wf.ProjectsRoot),
		TmuxSession:  "tickets-" + name,
	}
	if wf.WorkRoot == "" {
		l.WorkRoot = filepath.Join(config.Home(), "work", name)
	}
	if wf.ProjectsRoot == "" {
		l.ProjectsRoot = filepath.Join(config.Home(), "Projects")
	}
	return l
}

// Context is a resolved vault and its locations.
type Context struct {
	Vault string
	Locations
}

// Require resolves the current vault and exports it as
// CLAUDE_TICKETS_VAULT for the commands and sessions this one starts.
func Require() (Context, error) {
	v, err := Current()
	if err != nil {
		return Context{}, err
	}
	os.Setenv("CLAUDE_TICKETS_VAULT", v)
	return Context{v, LocationsOf(v)}, nil
}

// PathsOverlap reports whether a and b are the same or one is inside the
// other (after ~ expansion, symlinks resolved where they exist).
func PathsOverlap(a, b string) bool {
	a, b = canonical(a), canonical(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func canonical(p string) string {
	p = filepath.Clean(config.ExpandHome(p))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	// realpath -m: resolve the longest existing prefix
	dir, rest := p, ""
	for dir != "/" && dir != "." {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = filepath.Dir(dir)
	}
	return p
}

// Optional is the current vault when there is one (exported as
// CLAUDE_TICKETS_VAULT, as Require does), else no vault and the default
// locations (~/work, ~/Projects), for the commands that work without one.
func Optional() Context {
	v, ok, _ := Default()
	if !ok {
		if vs := List(); len(vs) == 1 {
			v, ok = filepath.Join(config.ObsidianRoot(), vs[0]), true
		}
	}
	if ok {
		os.Setenv("CLAUDE_TICKETS_VAULT", v)
		return Context{v, LocationsOf(v)}
	}
	home := config.Home()
	return Context{Locations: Locations{
		WorkRoot:     filepath.Join(home, "work"),
		ProjectsRoot: filepath.Join(home, "Projects"),
		TmuxSession:  "tickets",
	}}
}
