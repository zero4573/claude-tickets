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

	"github.com/zero4573/tix/internal/cli"
	"github.com/zero4573/tix/internal/gitx"
	"github.com/zero4573/tix/internal/tasksplugin"
	"github.com/zero4573/tix/internal/workspace"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"tix": func() { overrideContainer(); cli.Main() },
		// tix-tty is tix with stdin and stdout taken for a terminal (testscript
		// gives it pipes), for the sessions that run in this terminal
		"tix-tty": func() { overrideContainer(); cli.AssumeTerminal(); cli.Main() },
	})
}

// overrideContainer makes tix's container check follow $TIX_TEST_CONTAINER,
// which only Setup sets ("0" by default: the host; a script sets "1" to see
// tix refuse in a container). The shipped binary never reads it.
func overrideContainer() {
	switch os.Getenv("TIX_TEST_CONTAINER") {
	case "0":
		gitx.ContainerOverride = func() bool { return false }
	case "1":
		gitx.ContainerOverride = func() bool { return true }
	}
}

// stubs are fake tools on PATH: claude, notify-send and editor
// record their arguments in $WORK/calls/<name>.
var stubs = map[string]string{
	// claude: one line of arguments per run (and its cwd, vault, dev bin,
	// PATH and PID); with -p, a stream-json conversation
	"claude": `#!/bin/sh
echo "$@" >> "$WORK/calls/claude"
pwd >> "$WORK/calls/claude-cwd"
echo "TIX_VAULT=$TIX_VAULT" >> "$WORK/calls/claude-env"
echo "TIX_DEV_BIN=$TIX_DEV_BIN" >> "$WORK/calls/claude-env"
echo "PATH=$PATH" >> "$WORK/calls/claude-env"
echo "$$" >> "$WORK/calls/claude-pid"
# tix sync: keep the plan, and leave a follow-up as the skill would
if [ -f tickets/.sync-plan.json ]; then
  cat tickets/.sync-plan.json >> "$WORK/calls/sync-plans"
  echo "Check the HTML parts of the plan" > tickets/.sync-followups
fi
# /tix:save (tix clean --save): marks the ticket saved when $WORK/save-ok exists
case " $* " in
  *" -p /tix:save "*)
    if [ -f "$WORK/save-ok" ]; then
      id=$(basename "$PWD"); f=$(ls "$TIX_VAULT"/tickets/*/"$id/$id.md" "$TIX_VAULT/tickets/$id/$id.md" 2>/dev/null | head -n 1)
      { head -n 1 "$f"; echo "saved: $(date +%Y-%m-%dT%H:%M:%S)"; tail -n +2 "$f"; } > "$f.tmp" && mv "$f.tmp" "$f"
    fi ;;
esac
case " $* " in
  *" -p "*)
    echo '{"type":"system","subtype":"init"}'
    printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"Looking."},{"type":"tool_use","name":"Grep","input":{"pattern":"billing\nflow"}}]}}'
    echo '{"type":"result","is_error":false,"result":"It flows."}' ;;
esac
`,
	"notify-send": `#!/bin/sh
echo "$@" >> "$WORK/calls/notify-send"
`,
	// kiro-cli: one line of arguments per run, its cwd and the variables
	// tix sets for it
	"kiro-cli": `#!/bin/sh
echo "$@" >> "$WORK/calls/kiro"
pwd >> "$WORK/calls/kiro-cwd"
for v in TIX_VAULT SANDBOX_ADD_DIRS SANDBOX_RO_PATHS GOMODCACHE; do eval "echo $v=\$$v" >> "$WORK/calls/kiro-env"; done
`,
	"editor": `#!/bin/sh
echo "$@" >> "$WORK/calls/editor"
`,
	// git is the real one, except in a fake checkout (an empty .git dir):
	// status says it's dirty when it has a .dirty file
	"git": `#!/bin/sh
# tix puts "-c key=value" pairs (hooks and fsmonitor off) before "-C dir"
d=
prev=
for a in "$@"; do
  [ "$prev" = "-C" ] && d="$a"
  prev="$a"
done
case "$*" in
  *" status "*"--porcelain"*)
    if [ -d "$d/.git" ] && [ ! -e "$d/.git/HEAD" ]; then
      [ -f "$d/.dirty" ] && echo " M file"
      exit 0
    fi ;;
esac
exec "$REAL_GIT" "$@"
`,
}

// faketasks 404|bad: serves the Tasks plugin release for the rest of the
// script (TIX_TASKS_URL): every file is a 404, or bytes that
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
	ts.Setenv("TIX_TASKS_URL", s.URL)
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
			// session of workspace dir, as tix start does
			"live-session": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) != 1 {
					ts.Fatalf("usage: live-session <dir>")
				}
				ts.Check(workspace.RecordSession(ts.MkAbs(args[0]), "start"))
			},
			// samepid <dir>: the last session recorded in workspace dir has
			// the PID the claude stub last ran as (tix became claude: exec)
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
			// [container]: the scripts run as on the host (TIX_TEST_CONTAINER=0), so
			// it is false; it stays so a script's own `env TIX_TEST_CONTAINER=1`
			// (a tix that thinks it is in a container) doesn't change what runs
			if cond == "container" {
				return false, nil
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
			plugin := filepath.Join(work, "plugin")
			if err := os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"), []byte(`{"name": "tix"}`), 0o644); err != nil {
				return err
			}
			env.Setenv("TIX_PLUGIN", plugin)
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
			env.Setenv("TIX_VAULT", "")
			// the agent CLI and the sandbox folders come from the tests only
			for _, v := range []string{"TIX_AGENT", "TIX_KIRO", "SANDBOX_ADD_DIRS", "SANDBOX_RO_PATHS"} {
				env.Setenv(v, "")
			}
			env.Setenv("REAL_GIT", realGit)
			env.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			env.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(work, "gitconfig"))
			for _, who := range []string{"AUTHOR", "COMMITTER"} {
				env.Setenv("GIT_"+who+"_NAME", "Jane Doe")
				env.Setenv("GIT_"+who+"_EMAIL", "jane@example.com")
			}
			env.Setenv("TIX_TEST_CONTAINER", "0")
			// tix vault init never reaches the real Tasks releases: this
			// refuses at once (faketasks serves a fake one)
			env.Setenv("TIX_TASKS_URL", "http://127.0.0.1:1/tasks")
			// The pinned version and the settings tix writes, for fixtures
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
