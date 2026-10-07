package watch

import (
	"bytes"
	"io"
	"unicode/utf8"
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
	eraseLine    = "\x1b[K" // to the end of the line
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
// from the top left, in one write so it doesn't flicker. Each line shorter
// than width has the rest of the previous one erased (not a full-width
// one: erasing right after it would take its last character on terminals
// with a pending wrap), there's no newline after the last line so the
// screen never scrolls, and whatever a longer previous frame left below is
// erased.
func DrawFrame(w io.Writer, lines []string, width int) error {
	var b bytes.Buffer
	b.WriteString(home)
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l)
		if utf8.RuneCountInString(l) < width {
			b.WriteString(eraseLine)
		}
	}
	b.WriteString(eraseBelow)
	_, err := w.Write(b.Bytes())
	return err
}
