// Package watch keeps a text frame on a terminal and redraws it in place:
// the refresh loop of ct status --watch, fitting a frame to the terminal,
// and entering and leaving the alternate screen.
package watch

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The size assumed when the terminal's can't be read.
const fallbackWidth, fallbackHeight = 80, 24

// Fit makes lines fit a width x height terminal without wrapping or
// scrolling, so that redrawing in place stays aligned: control characters
// become spaces, lines are cut to width runes, and past height lines the
// last one shown is "... N more" (the first lines stay: what needs you
// sorts first). A height of 1 shows the first line only. A width or height
// below 1 means unknown, and 80x24 is used.
func Fit(lines []string, width, height int) []string {
	if width < 1 {
		width = fallbackWidth
	}
	if height < 1 {
		height = fallbackHeight
	}
	shown := lines
	more := ""
	switch {
	case len(lines) <= height:
	case height == 1:
		shown = lines[:1]
	default:
		shown = lines[:height-1]
		more = fmt.Sprintf("... %d more", len(lines)-len(shown))
	}
	out := make([]string, 0, len(shown)+1)
	for _, l := range shown {
		out = append(out, cut(sanitize(l), width))
	}
	if more != "" {
		out = append(out, cut(more, width))
	}
	return out
}

// sanitize replaces control characters (C0, DEL, C1) with spaces, so text
// such as a session's waiting reason can't move the cursor.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// cut keeps the first width runes of s.
func cut(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	n := 0
	for i := range s {
		if n == width {
			return s[:i]
		}
		n++
	}
	return s
}

// shortDuration is d as typed: 1m rather than 1m0s, 1h rather than 1h0m0s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
