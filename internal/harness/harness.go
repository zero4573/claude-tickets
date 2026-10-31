// Package harness is the agent CLI a session runs: Claude Code (claude) or
// kiro (kiro-cli). The commands that start sessions (tix start, spec,
// teach) describe the session once (Session) and the harness turns it into
// that CLI's files and command line:
//
//   - claude: .claude/settings.json (deny rules, Go env), trust in Claude's
//     config, CLAUDE.md, the plugin by --plugin-dir, folders by --add-dir
//   - kiro: .kiro/agents/tix.json (the session's agent: prompt, hooks),
//     the plugin's skills and agents rendered into .kiro/ (RenderKiro),
//     AGENTS.md, the Go env in the session's environment
//
// Both get the folders the session needs as SANDBOX_ADD_DIRS (read-write)
// and SANDBOX_RO_PATHS (read-only) in their environment, added to any the
// caller already set: a sandbox that honours them mounts them. Nothing else
// about a sandbox is known here. Which harness a vault's sessions use is
// Select's.
package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Names
const (
	Claude = "claude"
	Kiro   = "kiro"
)

// Session is what a command wants of a session, whichever CLI runs it.
type Session struct {
	Dir  string   // the workspace (the session's cwd)
	Dirs []string // more folders the session works in, read-write (the vault, the repos root)
	// ReadOnly is folders the session reads but never changes (the model cache)
	ReadOnly []string
	// ReposRoot set: the repos under it are read-only (spec and teach sessions)
	ReposRoot string
	// DenyShell is shell commands the session never runs ("git commit")
	DenyShell []string
	// Instructions is the workspace's instructions (CLAUDE.md for claude,
	// AGENTS.md for kiro), in Claude's form (/tix:<skill>); "" writes none
	Instructions string
	// SystemPrompt is added to the CLI's system prompt
	SystemPrompt string
	// Skill and Args: the first prompt runs the plugin skill; Prompt (when
	// set) is sent instead, as it is
	Skill, Args, Prompt string
	Continue            bool     // the workspace's last conversation
	Plugin              string   // the plugin dir: a dev workspace's, else the installed one
	Hooks               string   // the tix the hooks run (kiro), "" this one
	Settings            bool     // write the workspace's settings (ticket, spec and teach workspaces)
	Print               bool     // headless: the answer to the first prompt, then exit
	Extra               []string // more arguments of the CLI's own (before the prompt)
}

// Harness is one agent CLI.
type Harness interface {
	Name() string
	// Prepare writes the workspace's files for the CLI
	Prepare(s Session) error
	// Argv is the command line of an interactive session
	Argv(s Session) ([]string, error)
	// Env is set for the session (added to the caller's environment)
	Env(s Session) map[string]string
	// SkillRef is how a prompt runs the plugin skill: /tix:<skill> or /tix-<skill>
	SkillRef(skill string) string
}

// Get is the harness of name.
func Get(name string) (Harness, error) {
	switch name {
	case "", Claude:
		return claude{}, nil
	case Kiro:
		return kiro{}, nil
	}
	return nil, fmt.Errorf("unknown agent %q (claude or kiro)", name)
}

// Select is the harness a session runs: flag (--agent), else $TIX_AGENT,
// else the vault's .workflow.json "agent", else claude.
func Select(flag, vaultAgent string) (Harness, error) {
	for _, n := range []string{flag, os.Getenv("TIX_AGENT"), vaultAgent} {
		if n = strings.TrimSpace(n); n != "" {
			return Get(n)
		}
	}
	return claude{}, nil
}

// FirstPrompt is s's first prompt for h: Prompt, else the skill command.
func FirstPrompt(h Harness, s Session) string {
	if s.Prompt != "" {
		return s.Prompt
	}
	if s.Skill == "" {
		return ""
	}
	p := h.SkillRef(s.Skill)
	if s.Args != "" {
		p += " " + s.Args
	}
	return p
}

// sandboxEnv is SANDBOX_ADD_DIRS and SANDBOX_RO_PATHS for s, after the
// values already in the environment (colon-separated; no duplicates).
func sandboxEnv(s Session, readOnly ...string) map[string]string {
	rw := append([]string{}, s.Dirs...)
	ro := append(append([]string{}, s.ReadOnly...), readOnly...)
	if s.ReposRoot != "" {
		ro = append(ro, s.ReposRoot)
	}
	return map[string]string{
		"SANDBOX_ADD_DIRS": joinList(os.Getenv("SANDBOX_ADD_DIRS"), rw),
		"SANDBOX_RO_PATHS": joinList(os.Getenv("SANDBOX_RO_PATHS"), ro),
	}
}

func joinList(have string, add []string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(strings.Split(have, string(os.PathListSeparator)), add...) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if c := filepath.Clean(p); !seen[c] {
			seen[c] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// existing is the dirs of ds that exist.
func existing(ds []string) []string {
	var out []string
	for _, d := range ds {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
