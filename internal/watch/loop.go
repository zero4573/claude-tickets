package watch

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// Config is what Run needs; every dependency comes through it, so the loop
// can be tested without a terminal.
type Config struct {
	// Out is the terminal. Only Run's goroutine writes to it.
	Out io.Writer
	// Interval is the longest time from the end of one refresh to the start
	// of the next.
	Interval time.Duration
	// Poll is how often Changed and Size are checked (250ms if unset).
	Poll time.Duration
	// Refresh builds the frame's lines (under the refreshed line). It may be
	// slow: it runs in its own goroutine, one at a time, and must not write
	// to Out.
	Refresh func() []string
	// Changed, called just before each refresh starts, takes a baseline and
	// returns a check telling whether something changed since; a change
	// starts a refresh before the interval is up. Nil: no change detection.
	Changed func() func() bool
	// Size is the terminal's size; !ok (or nil) means 80x24.
	Size func() (width, height int, ok bool)
	// Resize fires when the terminal was resized (SIGWINCH); nil on
	// Windows, where the size is polled.
	Resize <-chan os.Signal
	// Now is the clock for the refreshed line (time.Now if nil).
	Now func() time.Time
}

const defaultPoll = 250 * time.Millisecond

// Run draws a frame right away and then keeps it current until ctx is
// done, when it returns nil at once, even in the middle of a refresh (that
// refresh is abandoned). A frame is a line "refreshed HH:MM:SS, every
// <interval> (Ctrl-C to quit)", with the time the refresh started, then
// Refresh's lines, fitted to the terminal.
//
// Refreshes never overlap. The next one starts Interval after one ends, or
// as soon as Changed's check reports a change (a change seen during a
// refresh starts another right after it). A size change redraws the last
// frame without refreshing. A write error on Out is returned.
func Run(ctx context.Context, c Config) error {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	poll := c.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	size := func() (int, int) {
		if c.Size == nil {
			return fallbackWidth, fallbackHeight
		}
		w, h, ok := c.Size()
		if !ok || w < 1 || h < 1 {
			return fallbackWidth, fallbackHeight
		}
		return w, h
	}

	type result struct {
		lines []string
		at    time.Time
	}
	// One refresh at a time, so a buffer of one never blocks the sender,
	// even one abandoned when ctx is done
	results := make(chan result, 1)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var (
		inFlight, pending bool
		check             func() bool
		frame             []string
		drawnW, drawnH    int
	)
	start := func() {
		timer.Stop()
		inFlight, pending = true, false
		if c.Changed != nil {
			check = c.Changed()
		}
		at := now()
		go func() { results <- result{c.Refresh(), at} }()
	}
	draw := func() error {
		drawnW, drawnH = size()
		return DrawFrame(c.Out, Fit(frame, drawnW, drawnH))
	}
	resized := func() error {
		if frame == nil {
			return nil
		}
		if w, h := size(); w != drawnW || h != drawnH {
			return draw()
		}
		return nil
	}
	header := func(at time.Time) string {
		return fmt.Sprintf("refreshed %s, every %s (Ctrl-C to quit)", at.Format("15:04:05"), shortDuration(c.Interval))
	}

	start()
	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-results:
			inFlight = false
			frame = append([]string{header(r.at)}, r.lines...)
			if err := draw(); err != nil {
				return err
			}
			if pending {
				start()
			} else {
				timer.Reset(c.Interval)
			}
		case <-timer.C:
			if !inFlight {
				start()
			}
		case <-ticker.C:
			if check != nil && !pending && check() {
				if inFlight {
					pending = true
				} else {
					start()
				}
			}
			if err := resized(); err != nil {
				return err
			}
		case <-c.Resize:
			if err := resized(); err != nil {
				return err
			}
		}
	}
}
