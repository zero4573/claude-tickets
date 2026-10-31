package harness

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zero4573/tix/internal/session"
)

// claude is Claude Code, through internal/session (its settings, trust and
// command line).
type claude struct{}

func (claude) Name() string                 { return Claude }
func (claude) SkillRef(skill string) string { return "/tix:" + skill }

func (claude) Prepare(s Session) error {
	if s.Settings {
		var deny []string
		if s.ReposRoot != "" {
			deny = append(deny, session.ReposReadOnly(s.ReposRoot)...)
		}
		for _, c := range s.DenyShell {
			deny = append(deny, "Bash("+c+":*)")
		}
		if err := session.WriteSettings(s.Dir, deny...); err != nil {
			return err
		}
		if err := session.Trust(s.Dir); err != nil {
			fmt.Fprintf(os.Stderr, "tix: couldn't mark %s as trusted in %s: %v\n", s.Dir, session.ClaudeConfig(), err)
		}
	}
	if s.Instructions != "" {
		return os.WriteFile(filepath.Join(s.Dir, "CLAUDE.md"), []byte(s.Instructions), 0o644)
	}
	return nil
}

func (h claude) Argv(s Session) ([]string, error) {
	plugin := s.Plugin
	if plugin == "" {
		var err error
		if plugin, err = session.PluginDir(); err != nil {
			return nil, err
		}
	}
	argv := session.CommandPlugin(plugin, claudeDirs(s)...)
	if s.SystemPrompt != "" {
		argv = append(argv, "--append-system-prompt", s.SystemPrompt)
	}
	if s.Settings {
		argv = append(argv, "--permission-mode", "auto")
	}
	if s.Continue {
		argv = append(argv, "--continue")
	}
	argv = append(argv, s.Extra...)
	if p := FirstPrompt(h, s); p != "" {
		if s.Print {
			argv = append(argv, "-p", p, "--output-format", "stream-json", "--verbose")
		} else {
			argv = append(argv, p)
		}
	}
	return argv, nil
}

// claudeDirs is every folder the session reads or writes, for --add-dir
// (the read-only ones are kept so by the deny rules): Dirs, ReposRoot,
// ReadOnly
func claudeDirs(s Session) []string {
	dirs := append([]string{}, s.Dirs...)
	if s.ReposRoot != "" {
		dirs = append(dirs, s.ReposRoot)
	}
	return append(dirs, s.ReadOnly...)
}

// Env: the folders as SANDBOX_* (claude's --add-dir mounts them too)
func (claude) Env(s Session) map[string]string {
	return sandboxEnv(Session{Dirs: existing(s.Dirs), ReadOnly: existing(s.ReadOnly), ReposRoot: s.ReposRoot})
}
