package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/watch"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// minInterval bounds how often ct status --watch recomputes the table (a
// git status per worktree, tmux): .agent-state changes still show sooner.
const minInterval = time.Second

// intervalFlag is --interval: a Go duration of at least minInterval.
type intervalFlag time.Duration

func (f *intervalFlag) String() string { return time.Duration(*f).String() }
func (f *intervalFlag) Type() string   { return "duration" }
func (f *intervalFlag) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d < minInterval {
		return fmt.Errorf("want a duration of at least %s, like 2s or 1m", minInterval)
	}
	*f = intervalFlag(d)
	return nil
}

// stdoutIsTerminal: watch mode draws on stdout and reads no input, so it
// only needs stdout to be a terminal (unlike isTerminal, which ct-tty fakes).
var stdoutIsTerminal = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// printStatusOnce is watch mode where it can't redraw: the table once, as
// plain ct status prints it, and why on stderr.
func printStatusOnce(ctx vault.Context, l launcher.Launcher, why string) error {
	if err := writeStatus(os.Stdout, buildStatus(ctx, l)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ct status: --watch needs %s; printed once\n", why)
	return nil
}

// runStatusWatch keeps ct status's table on the terminal (the alternate
// screen) until Ctrl-C or SIGTERM, which end it with status 0 and the
// terminal as it was.
func runStatusWatch(parent context.Context, ctx vault.Context, l launcher.Launcher, interval time.Duration) error {
	if parent == nil {
		parent = context.Background()
	}
	// First, so no signal can kill ct between entering the alternate screen
	// and being ready to leave it
	sigCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	out := os.Stdout
	restoreVT, err := watch.EnableVT(out)
	if err != nil {
		return printStatusOnce(ctx, l, "a terminal that handles escape sequences")
	}
	resize, stopResize := watch.ResizeSignals()
	defer stopResize()

	// workspace.Read's warnings go into the frame, not over it. Left in
	// place on return: an abandoned refresh may still be running, and ct
	// exits right after
	warns := &lineCollector{}
	workspace.Warnings = warns

	if err := watch.Enter(out); err != nil {
		restoreVT()
		return err
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = watch.Leave(out)
			restoreVT()
		})
	}
	defer func() {
		if p := recover(); p != nil {
			restore()
			panic(p)
		}
		restore()
	}()

	return watch.Run(sigCtx, watch.Config{
		Out:      out,
		Interval: interval,
		Refresh: func() []string {
			// A refresh runs in its own goroutine, where the defer above
			// can't catch a panic
			defer func() {
				if p := recover(); p != nil {
					restore()
					panic(p)
				}
			}()
			var buf bytes.Buffer
			_ = writeStatus(&buf, buildStatus(ctx, l))
			lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
			return append(lines, warns.drain()...)
		},
		Changed: func() func() bool {
			base := workspace.StateStamps(ctx.WorkRoot)
			return func() bool { return base.Changed(workspace.StateStamps(ctx.WorkRoot)) }
		},
		Size: func() (int, int, bool) {
			w, h, err := term.GetSize(int(out.Fd()))
			return w, h, err == nil
		},
		Resize: resize,
	})
}

// lineCollector keeps the lines written to it until drained, once each.
type lineCollector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *lineCollector) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *lineCollector) drain() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lines []string
	seen := map[string]bool{}
	for _, l := range strings.Split(c.buf.String(), "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" && !seen[l] {
			seen[l] = true
			lines = append(lines, l)
		}
	}
	c.buf.Reset()
	return lines
}

var errIntervalNeedsWatch = errors.New("--interval needs --watch")
