package watch

import (
	"bytes"
	"io"
)

// The escape sequences used (xterm, tmux, macOS terminals, Windows 10+
// consoles with VT processing).
const (
	altScreenOn  = "\x1b[?1049h"
	altScreenOff = "\x1b[?1049l"
	home         = "\x1b[H"
	clearScreen  = "\x1b[2J"
	hideCursor   = "\x1b[?25l"
	showCursor   = "\x1b[?25h"
	eraseLine    = "\x1b[K" // from the cursor to the end of the line
	eraseBelow   = "\x1b[J" // from the cursor to the end of the screen
)

// Enter switches to the alternate screen, clears it and hides the cursor.
func Enter(w io.Writer) error {
	_, err := io.WriteString(w, altScreenOn+home+clearScreen+hideCursor)
	return err
}

// Leave shows the cursor and goes back to the normal screen, as it was
// before Enter.
func Leave(w io.Writer) error {
	_, err := io.WriteString(w, showCursor+altScreenOff)
	return err
}

// DrawFrame draws lines (already fitted, see Fit) over the previous frame
// from the top left, in one write so it doesn't flicker. Every erase comes
// at the start of a row, before its text, never after it: after a line
// that fills the width the cursor sits on its last column with a pending
// wrap, and xterm-like terminals and Windows consoles erase from there
// inclusive, which would take that line's last character. So each row but
// the last is erased from its first column (ESC[K) and then written, and
// the last row is erased together with whatever a taller previous frame
// left below it (ESC[J) before its text is written. This doesn't depend on
// how wide the text really is. There's no newline after the last line, so
// the screen never scrolls.
func DrawFrame(w io.Writer, lines []string) error {
	var b bytes.Buffer
	b.WriteString(home)
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		if i == len(lines)-1 {
			b.WriteString(eraseBelow)
		} else {
			b.WriteString(eraseLine)
		}
		b.WriteString(l)
	}
	if len(lines) == 0 {
		b.WriteString(eraseBelow)
	}
	_, err := w.Write(b.Bytes())
	return err
}
