// Package workspace manages a ticket or kb session's workspace and its
// workspace.json: the ID, the vault, and the repos checked out in it.
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/zero4573/claude-tickets/internal/lock"
	"github.com/zero4573/claude-tickets/internal/tmuxx"
)

// Repo is one checkout in a workspace.
type Repo struct {
	Provider      string  `json:"provider"`
	Owner         string  `json:"owner"`
	Repo          string  `json:"repo"`
	Slug          string  `json:"slug"`
	Path          string  `json:"path"`
	Base          string  `json:"base"`
	Branch        string  `json:"branch"`
	Mode          string  `json:"mode"` // worktree or clone
	TargetVersion *string `json:"targetVersion"`
}

// Clone is the repo's main clone under the projects root.
func (r Repo) Clone() string { return r.Provider + "/" + r.Owner + "/" + r.Repo }

// File is a workspace's workspace.json.
func File(dir string) string { return filepath.Join(dir, "workspace.json") }

// Update changes a JSON file under a lock (so parallel writers, e.g.
// subagents adding worktrees, don't lose each other's changes): the
// document is decoded into a generic map, changed by fn, and written back.
func Update(file string, fn func(doc map[string]any) error) error {
	return lock.With(file+".lock", func() error {
		doc := map[string]any{}
		if data, err := os.ReadFile(file); err == nil && len(data) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return err
			}
		}
		if err := fn(doc); err != nil {
			return err
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, file)
	})
}

// Ensure creates the workspace dir and its workspace.json (id, no repos)
// if missing, keeping the repos already recorded; a vault, when given, is
// recorded too.
func Ensure(dir, id, vault string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return Update(File(dir), func(doc map[string]any) error {
		if _, ok := doc["id"]; !ok {
			doc["id"] = id
		}
		if _, ok := doc["repos"]; !ok {
			doc["repos"] = []any{}
		}
		if vault != "" {
			doc["vault"] = vault
		}
		return nil
	})
}

// Info is a workspace.json's content.
type Info struct {
	ID    string `json:"id"`
	Vault string `json:"vault"`
	Repos []Repo `json:"repos"`
}

// Read loads a workspace.json.
func Read(dir string) (Info, error) {
	var info Info
	data, err := os.ReadFile(File(dir))
	if err != nil {
		return info, err
	}
	return info, json.Unmarshal(data, &info)
}

// Running reports whether a workspace's session is running: its window is
// open in the vault's tmux session (ticket sessions), or its hooks' last
// state isn't exited and is less than a day old (kb sessions run in your
// own terminal).
func Running(dir, tmuxSession string) bool {
	if tmuxx.HasWindow(tmuxSession, filepath.Base(dir)) {
		return true
	}
	st, err := os.Stat(filepath.Join(dir, ".agent-state"))
	if err != nil || time.Since(st.ModTime()) > 24*time.Hour {
		return false
	}
	state := ReadState(dir)
	return state.State != "" && state.State != "exited"
}

// State is what the session's hooks last recorded (.agent-state).
type State struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	TS     string `json:"ts"`
}

// ReadState reads <dir>/.agent-state (zero State if missing).
func ReadState(dir string) State {
	var s State
	if data, err := os.ReadFile(filepath.Join(dir, ".agent-state")); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// List is every workspace under workRoot (ticket workspaces only, or kb
// ones too), as directories holding workspace.json.
func List(workRoot string, withKB bool) []string {
	all, _ := filepath.Glob(filepath.Join(workRoot, "*", "workspace.json"))
	var matches []string
	for _, m := range all {
		// Go's * also matches dot-dirs: those are kb workspaces
		if b := filepath.Base(filepath.Dir(m)); b[0] != '.' {
			matches = append(matches, m)
		}
	}
	if withKB {
		kb, _ := filepath.Glob(filepath.Join(workRoot, ".kb-*", "workspace.json"))
		matches = append(matches, kb...)
	}
	var dirs []string
	for _, m := range matches {
		dirs = append(dirs, filepath.Dir(m))
	}
	return dirs
}
