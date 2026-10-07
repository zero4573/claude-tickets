//go:build !windows

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

const (
	qaEnterSeq = "\x1b[?1049h\x1b[H\x1b[2J\x1b[?25l"
	qaLeaveSeq = "\x1b[?25h\x1b[?1049l"
)

// watchWithStdoutPipe runs runStatusWatch(parent, ...) with os.Stdout a
// pipe, waits for the first frame, calls during(), and returns what was
// written and runStatusWatch's result.
func watchWithStdoutPipe(t *testing.T, parent context.Context, during func()) (string, error) {
	t.Helper()
	t.Setenv(launcher.EnvVar, "none")
	l, err := launcher.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	vaultDir := filepath.Join(root, "a")
	work := filepath.Join(root, "work-a")
	mkWorkspace(t, vaultDir, work, "MAN-1", "working", "")
	ctx := vault.Context{Vault: vaultDir, Locations: vault.Locations{WorkRoot: work, TmuxSession: "tickets-a"}}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldWarn := os.Stdout, workspace.Warnings
	os.Stdout = w
	t.Cleanup(func() { os.Stdout, workspace.Warnings = oldOut, oldWarn })
	var out syncBuf
	copied := make(chan struct{})
	go func() { io.Copy(&out, r); close(copied) }()

	done := make(chan error, 1)
	go func() { done <- runStatusWatch(parent, ctx, l, time.Hour) }()
	// the first frame is drawn after signal.NotifyContext, so a signal sent
	// from here on can't kill the test binary
	waitFor(t, "the first frame", 2*time.Second, func() bool { return strings.Contains(out.lastFrame(), "MAN-1") })
	during()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runStatusWatch didn't return")
	}
	os.Stdout = oldOut
	w.Close()
	<-copied
	return out.String(), runErr
}

// QA (MAN-15), criterion 10: a real SIGINT or SIGTERM (not a cancelled
// context) ends watch mode with nil (exit 0), the cursor shown and the
// alternate screen left.
func TestRunStatusWatchSignals(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			out, err := watchWithStdoutPipe(t, context.Background(), func() {
				if err := syscall.Kill(os.Getpid(), sig); err != nil {
					t.Fatal(err)
				}
			})
			if err != nil {
				t.Errorf("runStatusWatch returned %v on %v, want nil", err, sig)
			}
			if !strings.HasPrefix(out, qaEnterSeq) {
				t.Errorf("doesn't start by entering the alternate screen: %q", out[:min(40, len(out))])
			}
			if !strings.HasSuffix(out, qaLeaveSeq) {
				t.Errorf("doesn't end by leaving it: ...%q", out[max(0, len(out)-40):])
			}
			if n := strings.Count(out, qaLeaveSeq); n != 1 {
				t.Errorf("left the alternate screen %d times", n)
			}
		})
	}
}

// panicLauncher is the none launcher, except that listing windows panics:
// a bug inside a refresh.
type panicLauncher struct{ launcher.Launcher }

func (panicLauncher) Windows(string) []string { panic("QA: a refresh panics") }

// QA (MAN-15), design section 6 / dev-notes "Panic in a refresh": a panic
// inside a refresh (its own goroutine) still restores the terminal before
// ct crashes. Run in a child process, since the panic ends it.
func TestRunStatusWatchPanicRestores(t *testing.T) {
	if os.Getenv("MAN15_QA_PANIC_CHILD") == "1" {
		t.Setenv(launcher.EnvVar, "none")
		l, err := launcher.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		vaultDir := filepath.Join(root, "a")
		work := filepath.Join(root, "work-a")
		mkWorkspace(t, vaultDir, work, "MAN-1", "working", "")
		ctx := vault.Context{Vault: vaultDir, Locations: vault.Locations{WorkRoot: work, TmuxSession: "tickets-a"}}
		_ = runStatusWatch(context.Background(), ctx, panicLauncher{l}, time.Hour)
		t.Fatal("runStatusWatch returned instead of crashing")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunStatusWatchPanicRestores$", "-test.count=1")
	cmd.Env = append(os.Environ(), "MAN15_QA_PANIC_CHILD=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the child didn't crash")
	}
	if err == nil {
		t.Fatalf("the child exited 0; stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "QA: a refresh panics") {
		t.Errorf("no panic message on stderr:\n%s", stderr.String())
	}
	out := stdout.String()
	if !strings.HasPrefix(out, qaEnterSeq) {
		t.Errorf("didn't enter the alternate screen: %q", out[:min(40, len(out))])
	}
	if !strings.Contains(out, qaLeaveSeq) {
		t.Errorf("the terminal wasn't restored before the crash: %q", out)
	}
}
