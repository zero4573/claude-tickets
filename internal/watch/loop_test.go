package watch

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// frames records each DrawFrame write as the frame's lines.
type frames struct {
	mu   sync.Mutex
	list [][]string
	err  error
}

func (f *frames) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	s := strings.TrimSuffix(strings.TrimPrefix(string(p), home), eraseBelow)
	s = strings.ReplaceAll(s, eraseLine, "")
	f.list = append(f.list, strings.Split(s, "\r\n"))
	return len(p), nil
}

func (f *frames) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.list)
}

func (f *frames) last() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.list) == 0 {
		return nil
	}
	return f.list[len(f.list)-1]
}

// eventually waits up to 2s for cond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// run starts Run in the background; stop cancels it and returns its error.
func run(t *testing.T, c Config) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c) }()
	var once sync.Once
	var err error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case err = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Run didn't return after cancel")
			}
		})
		return err
	}
	t.Cleanup(func() { stop() })
	return stop
}

// version is a fake .agent-state: Changed's check reports whether it was
// bumped since the baseline.
type version struct{ n atomic.Int64 }

func (v *version) bump() { v.n.Add(1) }
func (v *version) changed() func() bool {
	base := v.n.Load()
	return func() bool { return v.n.Load() != base }
}

func TestRunFirstFrame(t *testing.T) {
	out := &frames{}
	clock := time.Date(2026, 10, 7, 14, 3, 7, 0, time.Local)
	stop := run(t, Config{
		Out:      out,
		Interval: 2 * time.Second,
		Poll:     5 * time.Millisecond,
		Refresh:  func() []string { return []string{"Vault a, tmux session tickets-a", "TICKET  AGENT"} },
		Size:     func() (int, int, bool) { return 100, 30, true },
		Now:      func() time.Time { return clock },
	})
	eventually(t, "the first frame", func() bool { return out.count() >= 1 })
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	want := []string{"refreshed 14:03:07, every 2s (Ctrl-C to quit)", "Vault a, tmux session tickets-a", "TICKET  AGENT"}
	if got := out.last(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunInterval(t *testing.T) {
	const interval = 20 * time.Millisecond
	var mu sync.Mutex
	var starts, ends []time.Time
	stop := run(t, Config{
		Out:      &frames{},
		Interval: interval,
		Poll:     5 * time.Millisecond,
		Refresh: func() []string {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(3 * time.Millisecond)
			mu.Lock()
			ends = append(ends, time.Now())
			mu.Unlock()
			return nil
		},
	})
	eventually(t, "4 refreshes", func() bool { mu.Lock(); defer mu.Unlock(); return len(ends) >= 4 })
	stop()
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(ends[i-1]); gap < interval {
			t.Errorf("refresh %d started %v after the previous one ended, want at least %v", i, gap, interval)
		}
	}
}

func TestRunChangeTriggersRefresh(t *testing.T) {
	var v version
	var refreshes atomic.Int32
	stop := run(t, Config{
		Out:      &frames{},
		Interval: time.Hour,
		Poll:     5 * time.Millisecond,
		Refresh:  func() []string { refreshes.Add(1); return nil },
		Changed:  v.changed,
	})
	eventually(t, "the first refresh", func() bool { return refreshes.Load() == 1 })
	time.Sleep(30 * time.Millisecond)
	if n := refreshes.Load(); n != 1 {
		t.Fatalf("%d refreshes without a change, want 1", n)
	}
	v.bump()
	eventually(t, "a refresh after the change", func() bool { return refreshes.Load() == 2 })
	stop()
}

// Changes during a refresh don't start another one alongside it; they all
// make one refresh, right after.
func TestRunNoOverlap(t *testing.T) {
	var v version
	var calls, inFlight, maxInFlight atomic.Int32
	release := make(chan struct{})
	stop := run(t, Config{
		Out:      &frames{},
		Interval: time.Hour,
		Poll:     2 * time.Millisecond,
		Changed:  v.changed,
		Refresh: func() []string {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			if n > maxInFlight.Load() {
				maxInFlight.Store(n)
			}
			if calls.Add(1) == 1 {
				<-release
			}
			return nil
		},
	})
	eventually(t, "the first refresh", func() bool { return calls.Load() == 1 })
	for i := 0; i < 5; i++ {
		v.bump()
		time.Sleep(5 * time.Millisecond)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d refreshes started during the first one", n-1)
	}
	close(release)
	eventually(t, "the follow-up refresh", func() bool { return calls.Load() == 2 })
	time.Sleep(30 * time.Millisecond)
	stop()
	if n := calls.Load(); n != 2 {
		t.Errorf("%d refreshes, want 2 (one follow-up for all the changes)", n)
	}
	if m := maxInFlight.Load(); m != 1 {
		t.Errorf("%d refreshes ran at once", m)
	}
}

// The baseline is taken before a refresh, so a change made while it runs
// (here, by the time it ends) still makes another one.
func TestRunChangeDuringRefreshNotLost(t *testing.T) {
	var v version
	var calls atomic.Int32
	stop := run(t, Config{
		Out:      &frames{},
		Interval: time.Hour,
		Poll:     2 * time.Millisecond,
		Changed:  v.changed,
		Refresh: func() []string {
			if calls.Add(1) == 1 {
				v.bump()
			}
			return nil
		},
	})
	eventually(t, "a refresh for the change made during the first", func() bool { return calls.Load() == 2 })
	stop()
}

func TestRunResizeRedrawsWithoutRefresh(t *testing.T) {
	var width atomic.Int32
	width.Store(100)
	long := strings.Repeat("x", 60)
	for _, viaSignal := range []bool{false, true} {
		name := "poll"
		if viaSignal {
			name = "signal"
		}
		t.Run(name, func(t *testing.T) {
			width.Store(100)
			out := &frames{}
			var refreshes atomic.Int32
			resize := make(chan os.Signal, 1)
			poll := 5 * time.Millisecond
			if viaSignal {
				poll = time.Hour
			}
			stop := run(t, Config{
				Out:      out,
				Interval: time.Hour,
				Poll:     poll,
				Refresh:  func() []string { refreshes.Add(1); return []string{long} },
				Size:     func() (int, int, bool) { return int(width.Load()), 24, true },
				Resize:   resize,
			})
			eventually(t, "the first frame", func() bool { return out.count() == 1 })
			width.Store(10)
			if viaSignal {
				resize <- os.Interrupt
			}
			eventually(t, "a redraw", func() bool { return out.count() == 2 })
			stop()
			if n := refreshes.Load(); n != 1 {
				t.Errorf("%d refreshes, want 1 (a resize only redraws)", n)
			}
			for _, l := range out.last() {
				if utf8.RuneCountInString(l) > 10 {
					t.Errorf("line %q wider than the new width 10", l)
				}
			}
		})
	}
}

func TestRunCancelMidRefresh(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	started := make(chan struct{})
	stop := run(t, Config{
		Out:      &frames{},
		Interval: time.Hour,
		Poll:     5 * time.Millisecond,
		Refresh:  func() []string { close(started); <-block; return nil },
	})
	<-started
	if err := stop(); err != nil {
		t.Errorf("Run returned %v on cancel, want nil", err)
	}
}

func TestRunWriteError(t *testing.T) {
	boom := errors.New("terminal gone")
	err := Run(context.Background(), Config{
		Out:      &frames{err: boom},
		Interval: time.Hour,
		Poll:     5 * time.Millisecond,
		Refresh:  func() []string { return nil },
	})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}
