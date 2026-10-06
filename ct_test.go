package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/zero4573/claude-tickets/internal/cli"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/tasksplugin"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"ct": cli.Main,
		// ct-tty is ct with stdin and stdout taken for a terminal (testscript
		// gives it pipes), for the sessions that run in this terminal
		"ct-tty": func() { cli.AssumeTerminal(); cli.Main() },
	})
}

// stubs are fake tools on PATH: tmux, claude, podman, notify-send and editor
// record their arguments in $WORK/calls/<name>, and tmux lists the windows
// named in $WORK/tmux-windows.
var stubs = map[string]string{
	"tmux": `#!/bin/sh
echo "$@" >> "$WORK/calls/tmux"
case "$1" in
  list-windows) [ -f "$WORK/tmux-windows" ] && cat "$WORK/tmux-windows" || exit 1 ;;
  has-session) [ -f "$WORK/tmux-windows" ] ;;
esac
`,
	// claude: one line of arguments per run (and its cwd, vault and PID); with
	// -p, a stream-json conversation
	"claude": `#!/bin/sh
echo "$@" >> "$WORK/calls/claude"
pwd >> "$WORK/calls/claude-cwd"
echo "CLAUDE_TICKETS_VAULT=$CLAUDE_TICKETS_VAULT" >> "$WORK/calls/claude-env"
echo "$$" >> "$WORK/calls/claude-pid"
# ct sync: keep the plan, and leave a follow-up as the skill would
if [ -f tickets/.sync-plan.json ]; then
  cat tickets/.sync-plan.json >> "$WORK/calls/sync-plans"
  echo "Check the HTML parts of the plan" > tickets/.sync-followups
fi
# /tickets:save (ct clean --save): marks the ticket saved when $WORK/save-ok exists
case " $* " in
  *" -p /tickets:save "*)
    if [ -f "$WORK/save-ok" ]; then
      id=$(basename "$PWD"); f="$CLAUDE_TICKETS_VAULT/tickets/$id/$id.md"
      { head -n 1 "$f"; echo "saved: $(date +%Y-%m-%dT%H:%M:%S)"; tail -n +2 "$f"; } > "$f.tmp" && mv "$f.tmp" "$f"
    fi ;;
esac
case " $* " in
  *" -p "*)
    echo '{"type":"system","subtype":"init"}'
    echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Looking."},{"type":"tool_use","name":"mcp__graphify__query_graph","input":{"query":"billing\nflow"}}]}}'
    echo '{"type":"result","is_error":false,"result":"It flows."}' ;;
esac
`,
	"notify-send": `#!/bin/sh
echo "$@" >> "$WORK/calls/notify-send"
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

// faketasks 404|bad: serves the Tasks plugin release for the rest of the
// script (CLAUDE_TICKETS_TASKS_URL): every file is a 404, or bytes that
// don't match the pinned sha256s. Each request's path is appended to
// $WORK/calls/tasks.
func faketasks(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 1 || (args[0] != "404" && args[0] != "bad") {
		ts.Fatalf("usage: faketasks 404|bad")
	}
	mode, calls := args[0], filepath.Join(ts.Getenv("WORK"), "calls", "tasks")
	var mu sync.Mutex
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if f, err := os.OpenFile(calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, r.URL.Path)
			f.Close()
		}
		mu.Unlock()
		if mode == "404" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, "not the release")
	}))
	ts.Defer(s.Close)
	ts.Setenv("CLAUDE_TICKETS_TASKS_URL", s.URL)
}

func TestScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the CLI tests use sh stubs; the unit tests cover Windows")
	}
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// expand <file>...: replaces $VARS (e.g. $WORK) in files, in place
			"expand": func(ts *testscript.TestScript, neg bool, args []string) {
				for _, f := range args {
					ts.Check(os.WriteFile(ts.MkAbs(f), []byte(os.Expand(ts.ReadFile(f), ts.Getenv)), 0o644))
				}
			},
			"fakejira":  fakejira,
			"faketasks": faketasks,
			"cmpvault":  cmpvault,
			// live-session <dir>: records this (running) test process as the
			// session of workspace dir, as ct start does without a multiplexer
			"live-session": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: live-session <dir>")
				}
				ts.Check(workspace.RecordSession(ts.MkAbs(args[0]), "start"))
			},
			// samepid <dir>: the last session recorded in workspace dir has
			// the PID the claude stub last ran as (ct became claude: exec)
			"samepid": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 1 {
					ts.Fatalf("usage: samepid <dir>")
				}
				var doc struct {
					Sessions []struct {
						PID int `json:"pid"`
					} `json:"sessions"`
				}
				ts.Check(json.Unmarshal([]byte(ts.ReadFile(filepath.Join(args[0], ".sessions.json"))), &doc))
				pids := strings.Fields(ts.ReadFile("calls/claude-pid"))
				if len(doc.Sessions) == 0 || len(pids) == 0 {
					ts.Fatalf("no session recorded, or claude never ran")
				}
				rec, ran := strconv.Itoa(doc.Sessions[len(doc.Sessions)-1].PID), pids[len(pids)-1]
				if (rec == ran) == neg {
					ts.Fatalf("recorded pid %s, claude ran as pid %s", rec, ran)
				}
			},
		},
		Condition: func(cond string) (bool, error) {
			// [container]: inside a container, where ct ws gc refuses to run
			// and recorded PIDs can't be checked
			if cond == "container" {
				return gitx.InContainer(), nil
			}
			return false, fmt.Errorf("unknown condition %q", cond)
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
			plugin := filepath.Join(work, "plugin")
			if err := os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"), []byte(`{"name": "tickets"}`), 0o644); err != nil {
				return err
			}
			env.Setenv("CLAUDE_TICKETS_PLUGIN", plugin)
			syncData, err := filepath.Abs(filepath.Join("testdata", "sync"))
			if err != nil {
				return err
			}
			env.Setenv("SYNCDATA", syncData)
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
			env.Setenv("CLAUDE_TICKETS_LAUNCHER", "")
			env.Setenv("REAL_GIT", realGit)
			env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			env.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(work, "gitconfig"))
			for _, who := range []string{"AUTHOR", "COMMITTER"} {
				env.Setenv("GIT_"+who+"_NAME", "Jane Doe")
				env.Setenv("GIT_"+who+"_EMAIL", "jane@example.com")
			}
			env.Setenv("CLAUDE_TICKETS_CONTAINER", "podman")
			env.Setenv("CLAUDE_TICKETS_GRAPH_DIR", graphDir)
			// ct vault init never reaches the real Tasks releases: this
			// refuses at once (faketasks serves a fake one)
			env.Setenv("CLAUDE_TICKETS_TASKS_URL", "http://127.0.0.1:1/tasks")
			// The pinned version and the settings ct writes, for fixtures
			pin, err := tasksplugin.Embedded()
			if err != nil {
				return err
			}
			env.Setenv("TASKS_VERSION", pin.Version)
			settings, err := filepath.Abs(filepath.Join("assets", "obsidian", "tasks-settings.json"))
			if err != nil {
				return err
			}
			env.Setenv("TASKS_SETTINGS", settings)
			return nil
		},
	})
}
