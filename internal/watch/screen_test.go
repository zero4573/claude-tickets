package watch

import (
	"bytes"
	"strings"
	"testing"
)

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestDrawFrame(t *testing.T) {
	var w countingWriter
	if err := DrawFrame(&w, []string{"short", "exactly10!", "x"}); err != nil {
		t.Fatal(err)
	}
	if w.writes != 1 {
		t.Errorf("%d writes, want 1", w.writes)
	}
	// each row erased before its text, the last one with everything below
	// it, and no newline after the last one
	want := "\x1b[H" + "\x1b[Kshort" + "\r\n" + "\x1b[Kexactly10!" + "\r\n" + "\x1b[Jx"
	if got := w.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDrawFrameEmpty(t *testing.T) {
	var w bytes.Buffer
	if err := DrawFrame(&w, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[H\x1b[J"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// screen is a w x h xterm-like terminal for the sequences DrawFrame writes:
// autowrap with a pending wrap after the last column, which CR, LF and the
// erases don't take the cursor out of, and erases from the cursor
// inclusive. Enough to see what a frame leaves on the screen.
type screen struct {
	cells   [][]rune
	x, y    int
	pending bool // the cursor is on the last column, past a written char
}

func newScreen(w, h int) *screen {
	s := &screen{cells: make([][]rune, h)}
	for i := range s.cells {
		s.cells[i] = []rune(strings.Repeat(" ", w))
	}
	return s
}

func (s *screen) lf() {
	if s.y < len(s.cells)-1 {
		s.y++
		return
	}
	copy(s.cells, s.cells[1:])
	s.cells[len(s.cells)-1] = []rune(strings.Repeat(" ", len(s.cells[0])))
}

func (s *screen) write(t *testing.T, out string) {
	t.Helper()
	w := len(s.cells[0])
	for rs := []rune(out); len(rs) > 0; {
		switch {
		case strings.HasPrefix(string(rs), home):
			s.x, s.y, s.pending = 0, 0, false
			rs = rs[len(home):]
		case strings.HasPrefix(string(rs), eraseLine):
			for x := s.x; x < w; x++ {
				s.cells[s.y][x] = ' '
			}
			rs = rs[len(eraseLine):]
		case strings.HasPrefix(string(rs), eraseBelow):
			for y := s.y; y < len(s.cells); y++ {
				from := 0
				if y == s.y {
					from = s.x
				}
				for x := from; x < w; x++ {
					s.cells[y][x] = ' '
				}
			}
			rs = rs[len(eraseBelow):]
		case rs[0] == '\x1b':
			t.Fatalf("unexpected escape sequence in %q", string(rs))
		case rs[0] == '\r':
			s.x, s.pending = 0, false
			rs = rs[1:]
		case rs[0] == '\n':
			s.lf()
			s.pending = false
			rs = rs[1:]
		default:
			if s.pending {
				s.x, s.pending = 0, false
				s.lf()
			}
			s.cells[s.y][s.x] = rs[0]
			if s.x == w-1 {
				s.pending = true
			} else {
				s.x++
			}
			rs = rs[1:]
		}
	}
}

func (s *screen) rows() []string {
	var out []string
	for _, r := range s.cells {
		out = append(out, strings.TrimRight(string(r), " "))
	}
	return out
}

func drawOn(t *testing.T, s *screen, lines ...string) {
	t.Helper()
	var b bytes.Buffer
	if err := DrawFrame(&b, lines); err != nil {
		t.Fatal(err)
	}
	s.write(t, b.String())
}

func equalRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot  %q\nwant %q", what, got, want)
	}
}

// D1 in tickets/MAN-15/qa-report.md: a full-width last line keeps its last
// character, also when it fills the screen's last row.
func TestDrawFrameFullWidthLastLineOnScreen(t *testing.T) {
	s := newScreen(10, 4)
	drawOn(t, s, "short", "exactly10!")
	equalRows(t, "fits the height", s.rows(), []string{"short", "exactly10!", "", ""})

	s = newScreen(10, 2)
	drawOn(t, s, "first line", "exactly10!")
	equalRows(t, "fills the screen", s.rows(), []string{"first line", "exactly10!"})
	drawOn(t, s, "again 10..", "last 10...")
	equalRows(t, "redrawn", s.rows(), []string{"again 10..", "last 10..."})
}

// A shorter or narrower frame leaves nothing of the previous one: not to
// the right of its lines, not in the rows below.
func TestDrawFrameClearsStaleRows(t *testing.T) {
	s := newScreen(10, 5)
	drawOn(t, s, "aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc", "dddddddddd", "eeeeeeeeee")
	drawOn(t, s, "xy", "full-width")
	equalRows(t, "shorter frame", s.rows(), []string{"xy", "full-width", "", "", ""})
	drawOn(t, s, "1", "2", "3")
	equalRows(t, "taller again", s.rows(), []string{"1", "2", "3", "", ""})
	drawOn(t, s)
	equalRows(t, "empty frame", s.rows(), []string{"", "", "", "", ""})
}

func TestEnterLeave(t *testing.T) {
	var w bytes.Buffer
	if err := Enter(&w); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[?1049h\x1b[H\x1b[2J\x1b[?25l"; got != want {
		t.Errorf("Enter wrote %q, want %q", got, want)
	}
	w.Reset()
	if err := Leave(&w); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[?25h\x1b[?1049l"; got != want {
		t.Errorf("Leave wrote %q, want %q", got, want)
	}
}
