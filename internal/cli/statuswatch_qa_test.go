package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// syncBuf collects what a reader goroutine copies from a pipe.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// lastFrame is the text of the last frame drawn (from its cursor-home on),
// without the erases at the start of its rows.
func (s *syncBuf) lastFrame() string {
	out := s.String()
	if i := strings.LastIndex(out, "\x1b[H"); i >= 0 {
		return strings.NewReplacer("\x1b[K", "", "\x1b[J", "").Replace(out[i:])
	}
	return ""
}

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", within, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func mkWorkspace(t *testing.T, vaultDir, work, id, state, repoPath string) string {
	t.Helper()
	ws := filepath.Join(work, id)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, "tickets", id), 0o755); err != nil {
		t.Fatal(err)
	}
	note := "---\nsummary: x\nstatus: in-progress\nsource: manual\n---\n"
	if err := os.WriteFile(filepath.Join(vaultDir, "tickets", id, id+".md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	repos := "[]"
	if repoPath != "" {
		repos = `[{"provider":"github","owner":"me","repo":"x","slug":"github-me-x","path":"` + repoPath + `"}]`
	}
	if err := os.WriteFile(filepath.Join(ws, "workspace.json"), []byte(`{"id":"`+id+`","vault":"`+vaultDir+`","repos":`+repos+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if state != "" {
		hookWrite(t, ws, state)
	}
	return ws
}

// hookWrite writes .agent-state the way ct hook agent-state does: a tmp
// file renamed over it.
func hookWrite(t *testing.T, ws, state string) {
	t.Helper()
	tmp := filepath.Join(ws, ".agent-state.tmp")
	data := `{"state":"` + state + `","reason":"r-` + state + `","ts":"` + time.Now().Format(time.RFC3339) + `"}` + "\n"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(ws, ".agent-state")); err != nil {
		t.Fatal(err)
	}
}

// QA (MAN-15): runStatusWatch end to end, with stdout a pipe (so 80x24),
// the none launcher and a temp work root: it enters the alternate screen,
// redraws on an .agent-state rename well within 1s although the interval
// is an hour (criterion 6), re-sorts (9), follows workspaces coming and
// going (7), keeps workspace.Read's warnings in the frame rather than on
// stderr (design section 7), and on cancel leaves the screen and returns
// nil (10).
func TestRunStatusWatchEndToEnd(t *testing.T) {
	t.Setenv(launcher.EnvVar, "none")
	res, err := launcher.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	vaultDir := filepath.Join(root, "a")
	work := filepath.Join(root, "work-a")
	mkWorkspace(t, vaultDir, work, "MAN-1", "working", filepath.Join(root, "elsewhere"))
	ws2 := mkWorkspace(t, vaultDir, work, "MAN-2", "working", "")
	ctx := vault.Context{Vault: vaultDir, Locations: vault.Locations{WorkRoot: work, TmuxSession: "tickets-a"}}

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr, oldWarn := os.Stdout, os.Stderr, workspace.Warnings
	os.Stdout, os.Stderr, workspace.Warnings = outW, errW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr, workspace.Warnings = oldOut, oldErr, oldWarn })

	var out, stderr syncBuf
	copied := make(chan struct{}, 2)
	go func() { io.Copy(&out, outR); copied <- struct{}{} }()
	go func() { io.Copy(&stderr, errR); copied <- struct{}{} }()

	pctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runStatusWatch(pctx, ctx, res, time.Hour) }()

	waitFor(t, "the first frame", 2*time.Second, func() bool { return strings.Contains(out.lastFrame(), "MAN-2") })
	first := out.lastFrame()
	if !strings.HasPrefix(out.String(), "\x1b[?1049h\x1b[H\x1b[2J\x1b[?25l") {
		t.Errorf("doesn't start by entering the alternate screen: %q", out.String()[:min(40, len(out.String()))])
	}
	if !strings.Contains(first, "refreshed ") || !strings.Contains(first, "every 1h (Ctrl-C to quit)") {
		t.Errorf("no refreshed line in %q", first)
	}
	if !strings.Contains(first, "Vault a, no terminal multiplexer") {
		t.Errorf("header isn't plain ct status's: %q", first)
	}
	// cut to the 80 columns of the fallback size, so only its start is sure
	// to show (the temp path is long)
	if !strings.Contains(first, "\r\nct: ") {
		t.Errorf("workspace.Read's warning isn't in the frame: %q", first)
	}

	// MAN-2 turns needs-input through the hook's rename: redrawn within 1s
	// and sorted above MAN-1
	start := time.Now()
	hookWrite(t, ws2, "needs-input")
	waitFor(t, "MAN-2 needs-input on screen", time.Second, func() bool {
		f := out.lastFrame()
		i, j := strings.Index(f, "MAN-2"), strings.Index(f, "MAN-1")
		return strings.Contains(f, "needs-input") && i >= 0 && j >= 0 && i < j
	})
	t.Logf(".agent-state rename shown after %v", time.Since(start))

	// a workspace removed, then all of them: the empty line, without a restart
	if err := os.RemoveAll(ws2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "MAN-2 gone", time.Second, func() bool { return !strings.Contains(out.lastFrame(), "MAN-2") })
	if err := os.RemoveAll(filepath.Join(work, "MAN-1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the empty line", time.Second, func() bool { return strings.Contains(out.lastFrame(), "No ticket workspaces under") })
	// and a new one appears
	mkWorkspace(t, vaultDir, work, "MAN-3", "idle", "")
	waitFor(t, "MAN-3", time.Second, func() bool { return strings.Contains(out.lastFrame(), "MAN-3") })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runStatusWatch returned %v on cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runStatusWatch didn't return after cancel")
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	outW.Close()
	errW.Close()
	<-copied
	<-copied
	if s := out.String(); !strings.HasSuffix(s, "\x1b[?25h\x1b[?1049l") {
		t.Errorf("doesn't end by showing the cursor and leaving the alternate screen: ...%q", s[max(0, len(s)-40):])
	}
	if s := stderr.String(); s != "" {
		t.Errorf("wrote to stderr during watch (would land on the screen): %q", s)
	}
}

// QA (MAN-15): --interval's flag value keeps the last valid value on a bad
// Set and prints back as a duration (help shows "(default 2s)").
func TestIntervalFlagBoundaries(t *testing.T) {
	for in, ok := range map[string]bool{
		"1s": true, "1000ms": true, "1m30s": true, "24h": true,
		"999999999ns": false, "1": false, " 2s": false, "2 s": false, "+1s": true,
	} {
		f := intervalFlag(2 * time.Second)
		err := f.Set(in)
		if (err == nil) != ok {
			t.Errorf("Set(%q): err %v, want ok=%v", in, err, ok)
		}
	}
}
