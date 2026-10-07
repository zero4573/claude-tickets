package watch

import (
	"context"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// QA (MAN-15): whatever the lines and size, Fit never gives more than height
// lines, none wider than width, no control characters, and keeps the first
// lines in order.
func TestFitInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(15))
	alphabet := []rune("ab é漢\x1b\r\t\x7f\u0085-")
	for i := 0; i < 2000; i++ {
		lines := make([]string, rng.Intn(40))
		for j := range lines {
			r := make([]rune, rng.Intn(150))
			for k := range r {
				r[k] = alphabet[rng.Intn(len(alphabet))]
			}
			lines[j] = string(r)
		}
		w, h := rng.Intn(120)-5, rng.Intn(50)-5
		got := Fit(lines, w, h)
		if w < 1 {
			w = fallbackWidth
		}
		if h < 1 {
			h = fallbackHeight
		}
		if len(got) > h {
			t.Fatalf("%d lines for height %d", len(got), h)
		}
		if len(lines) <= h && len(got) != len(lines) {
			t.Fatalf("%d lines fit in %d but got %d", len(lines), h, len(got))
		}
		for j, l := range got {
			if n := utf8.RuneCountInString(l); n > w {
				t.Fatalf("line %d has %d runes, width %d", j, n, w)
			}
			for _, r := range l {
				if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
					t.Fatalf("control character %U left in %q", r, l)
				}
			}
			if j == len(got)-1 && len(lines) > h && h > 1 {
				more := "... " + strconv.Itoa(len(lines)-(h-1)) + " more"
				if l != cut(more, w) {
					t.Fatalf("last line %q, want %q", l, cut(more, w))
				}
				continue
			}
			if want := cut(sanitize(lines[j]), w); l != want {
				t.Fatalf("line %d = %q, want %q", j, l, want)
			}
		}
	}
}

// QA (MAN-15): Run works with no change detection and no size (80x24), on
// the interval alone, and the refreshed line shows the interval as typed.
func TestRunNilChangedAndSize(t *testing.T) {
	out := &frames{}
	var refreshes atomic.Int32
	stop := run(t, Config{
		Out:      out,
		Interval: 10 * time.Millisecond,
		Poll:     2 * time.Millisecond,
		Refresh:  func() []string { refreshes.Add(1); return []string{strings.Repeat("x", 200)} },
	})
	eventually(t, "3 interval refreshes", func() bool { return refreshes.Load() >= 3 })
	stop()
	f := out.last()
	if !strings.HasSuffix(f[0], ", every 10ms (Ctrl-C to quit)") {
		t.Errorf("refreshed line %q", f[0])
	}
	if n := utf8.RuneCountInString(f[1]); n != fallbackWidth {
		t.Errorf("line of %d runes without a size, want %d", n, fallbackWidth)
	}
}

func TestRunRefreshedLineInterval(t *testing.T) {
	for d, want := range map[time.Duration]string{time.Minute: "every 1m (", 90 * time.Second: "every 1m30s (", time.Hour: "every 1h ("} {
		out := &frames{}
		stop := run(t, Config{Out: out, Interval: d, Poll: time.Hour, Refresh: func() []string { return nil }})
		eventually(t, "the first frame", func() bool { return out.count() == 1 })
		stop()
		if got := out.last()[0]; !strings.Contains(got, want) {
			t.Errorf("interval %v: %q doesn't contain %q", d, got, want)
		}
	}
}

// QA (MAN-15): once Run has returned (Ctrl-C mid-refresh), the abandoned
// refresh finishing doesn't draw over the restored terminal.
func TestRunNoDrawAfterCancel(t *testing.T) {
	out := &frames{}
	release := make(chan struct{})
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			Out:      out,
			Interval: time.Hour,
			Poll:     time.Hour,
			Refresh: func() []string {
				if calls.Add(1) == 1 {
					<-release
				}
				return []string{"late"}
			},
		})
	}()
	eventually(t, "the refresh to start", func() bool { return calls.Load() == 1 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	if n := out.count(); n != 0 {
		t.Errorf("%d frames drawn after Run returned", n)
	}
}

// QA (MAN-15): a refresh slower than the interval: the interval timer never
// starts a second one alongside it.
func TestRunSlowRefreshNoOverlap(t *testing.T) {
	var inFlight, maxInFlight, calls atomic.Int32
	stop := run(t, Config{
		Out:      &frames{},
		Interval: time.Millisecond,
		Poll:     time.Millisecond,
		Refresh: func() []string {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			if n > maxInFlight.Load() {
				maxInFlight.Store(n)
			}
			calls.Add(1)
			time.Sleep(15 * time.Millisecond)
			return nil
		},
	})
	eventually(t, "5 refreshes", func() bool { return calls.Load() >= 5 })
	stop()
	if m := maxInFlight.Load(); m != 1 {
		t.Errorf("%d refreshes at once", m)
	}
}

// QA (MAN-15), defect D1 in tickets/MAN-15/qa-report.md: after a line that
// fills the width the cursor sits on its last column with a pending wrap,
// and an erase there (ESC[K, which DrawFrame avoids for that reason, or
// the final ESC[J) takes that line's last character on xterm-like
// terminals. Today the frame always ends with ESC[J, so when its last line
// is cut to the width (a wide table in a narrow pane that fits in height)
// its last character is erased. Unskip once fixed.
func TestDrawFrameNoEraseAfterFullWidthLastLine(t *testing.T) {
	t.Skip("defect D1 (MAN-15 QA): ESC[J follows a full-width last line")
	var w countingWriter
	if err := DrawFrame(&w, []string{"short", "exactly10!"}, 10); err != nil {
		t.Fatal(err)
	}
	if s := w.String(); strings.Contains(s, "exactly10!\x1b[J") || strings.Contains(s, "exactly10!\x1b[K") {
		t.Errorf("an erase right after the full-width last line: %q", s)
	}
}
