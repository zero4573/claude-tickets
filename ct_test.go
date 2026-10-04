package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/zero4573/claude-tickets/internal/cli"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"ct": cli.Main})
}

// stubs are fake tools on PATH: tmux, podman and editor record their arguments in
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
	// git is the real one, except in a fake checkout (an empty .git dir):
	// status says it's dirty when it has a .dirty file
	"git": `#!/bin/sh
case "$*" in
  *"status --porcelain"*)
    if [ -d "$2/.git" ] && [ ! -e "$2/.git/HEAD" ]; then
      [ -f "$2/.dirty" ] && echo " M file"
      exit 0
    fi ;;
esac
exec "$REAL_GIT" "$@"
`,
	// podman: images are "built" by touching $WORK/podman-built, export
	// gives a filesystem with the graphify server, and run (ct graph index)
	// reports each repo after "--" as ok, or as failed with $WORK/index-fail
	"podman": `#!/bin/sh
echo "$@" >> "$WORK/calls/podman"
case "$1" in
  info|rm) ;;
  image) [ -f "$WORK/podman-built" ] ;;
  build) touch "$WORK/podman-built" ;;
  create) echo cid1 ;;
  export)
    mkdir -p "$WORK/fakeroot/opt/graphify"
    echo '#!/bin/sh' > "$WORK/fakeroot/opt/graphify/serve.sh"
    chmod +x "$WORK/fakeroot/opt/graphify/serve.sh"
    tar -C "$WORK/fakeroot" -cf - . ;;
  run)
    cat > /dev/null
    seen=0
    for a in "$@"; do
      if [ $seen = 1 ]; then
        echo "== $a"
        if [ -f "$WORK/index-fail" ]; then echo "   failed"; else echo "   ok"; fi
      fi
      [ "$a" = -- ] && seen=1
    done
    [ ! -f "$WORK/index-fail" ] ;;
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
			realGit, err := exec.LookPath("git")
			if err != nil {
				return err
			}
			work := env.WorkDir
			home := filepath.Join(work, "home")
			stubDir := filepath.Join(work, "stub")
			for _, d := range []string{home, stubDir, filepath.Join(work, "calls")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
			}
			// The graphify image's sources (only hashed: podman is a stub)
			graphDir := filepath.Join(work, "graph-src")
			if err := os.MkdirAll(graphDir, 0o755); err != nil {
				return err
			}
			for _, f := range []string{"Containerfile", "graphify-requirements.txt", "serve.sh", "ticket-merge.py"} {
				if err := os.WriteFile(filepath.Join(graphDir, f), []byte(f+"\n"), 0o644); err != nil {
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
			env.Setenv("REAL_GIT", realGit)
			// git: no system config, $WORK/gitconfig as the user's, a fixed identity
			env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			env.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(work, "gitconfig"))
			for _, who := range []string{"AUTHOR", "COMMITTER"} {
				env.Setenv("GIT_"+who+"_NAME", "Jane Doe")
				env.Setenv("GIT_"+who+"_EMAIL", "jane@example.com")
			}
			env.Setenv("CLAUDE_TICKETS_CONTAINER", "podman")
			env.Setenv("CLAUDE_TICKETS_GRAPH_DIR", graphDir)
			return nil
		},
	})
}
