package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/zero4573/claude-tickets/internal/cli"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"ct": cli.Main})
}

// stubs are fake tools on PATH: each records its arguments in
// $WORK/calls/<name>, and tmux lists the windows named in
// $WORK/tmux-windows.
var stubs = map[string]string{
	"tmux": `#!/bin/sh
echo "$@" >> "$WORK/calls/tmux"
case "$1" in
  list-windows) [ -f "$WORK/tmux-windows" ] && cat "$WORK/tmux-windows" || exit 1 ;;
esac
`,
	"editor": `#!/bin/sh
echo "$@" >> "$WORK/calls/editor"
`,
	"git": `#!/bin/sh
case "$*" in
  *"status --porcelain"*) [ -f "$2/.dirty" ] && echo " M file" ;;
esac
`,
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// expand <file>...: replaces $VARS (e.g. $WORK) in files, in place
			"expand": func(ts *testscript.TestScript, neg bool, args []string) {
				for _, f := range args {
					ts.Check(os.WriteFile(ts.MkAbs(f), []byte(os.Expand(ts.ReadFile(f), ts.Getenv)), 0o644))
				}
			},
		},
		Setup: func(env *testscript.Env) error {
			work := env.WorkDir
			home := filepath.Join(work, "home")
			stubDir := filepath.Join(work, "stub")
			for _, d := range []string{home, stubDir, filepath.Join(work, "calls")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
			}
			for name, body := range stubs {
				if err := os.WriteFile(filepath.Join(stubDir, name), []byte(body), 0o755); err != nil {
					return err
				}
			}
			env.Setenv("HOME", home)
			env.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			env.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			env.Setenv("OBSIDIAN_ROOT", filepath.Join(home, "Documents", "Obsidian"))
			env.Setenv("PATH", stubDir+string(os.PathListSeparator)+env.Getenv("PATH"))
			env.Setenv("CLAUDE_TICKETS_VAULT", "")
			env.Setenv("TMUX", "")
			return nil
		},
	})
}
