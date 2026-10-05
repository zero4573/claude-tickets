package jira

import (
	"os"
	"strings"

	"github.com/zero4573/claude-tickets/internal/note"
)

const (
	blockStart = "<!-- source:start -->"
	blockEnd   = "<!-- source:end -->"
)

// parts is a note's source block: the table (head), ### Children,
// ### Description and ### Recent comments, always written in that order.
// Each is its lines' text, "" when absent.
type parts struct {
	head, children, description, comments string
}

func lines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// splitBlock reads a note's source block; ok is false when it has none.
func splitBlock(file string) (p parts, ok bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return p, false
	}
	all := lines(note.Normalize(string(data)))
	hasStart, hasEnd := false, false
	for _, l := range all {
		hasStart = hasStart || l == blockStart
		hasEnd = hasEnd || l == blockEnd
	}
	if !hasStart || !hasEnd {
		return p, false
	}
	var b [4]strings.Builder
	part, in := 0, false
	for _, l := range all {
		switch {
		case l == blockStart:
			in = true
			continue
		case l == blockEnd:
			in = false
			continue
		case !in:
			continue
		case l == "### Children":
			part = 1
		case l == "### Description":
			part = 2
		case l == "### Recent comments":
			part = 3
		}
		b[part].WriteString(l + "\n")
	}
	return parts{b[0].String(), b[1].String(), b[2].String(), b[3].String()}, true
}

func trimBlankTail(ls []string) []string {
	for len(ls) > 0 && ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// writeBlock replaces the note's source block with p (adding the block
// after the title heading when there's none).
func writeBlock(file string, p parts) error {
	var body []string
	for _, part := range []string{p.head, p.children, p.description, p.comments} {
		if part == "" {
			continue
		}
		// Each part ends with exactly one blank line, except the last
		body = append(append(body, trimBlankTail(strings.Split(strings.TrimSuffix(part, "\n"), "\n"))...), "")
	}
	block := append(append([]string{blockStart}, trimBlankTail(body)...), blockEnd)

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	all := lines(note.Normalize(string(data)))
	var out []string
	start, end := -1, -1
	for i, l := range all {
		if start < 0 && l == blockStart {
			start = i
		} else if start >= 0 && l == blockEnd {
			end = i
			break
		}
	}
	if start >= 0 {
		// A start marker with no end after it (edited away) is replaced on its
		// own: nothing after it is dropped
		if end < 0 {
			end = start
		}
		out = append(append(append(out, all[:start]...), block...), all[end+1:]...)
	} else {
		done := false
		for _, l := range all {
			out = append(out, l)
			if !done && strings.HasPrefix(l, "# ") {
				out = append(append(out, ""), block...)
				done = true
			}
		}
	}
	return os.WriteFile(file, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
