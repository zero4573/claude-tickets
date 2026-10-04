package jira

import (
	"os"
	"strings"
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
func splitBlock(note string) (p parts, ok bool) {
	data, err := os.ReadFile(note)
	if err != nil {
		return p, false
	}
	all := lines(string(data))
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
func writeBlock(note string, p parts) error {
	var body []string
	for _, part := range []string{p.head, p.children, p.description, p.comments} {
		if part == "" {
			continue
		}
		// Each part ends with exactly one blank line, except the last
		body = append(append(body, trimBlankTail(strings.Split(strings.TrimSuffix(part, "\n"), "\n"))...), "")
	}
	block := append(append([]string{blockStart}, trimBlankTail(body)...), blockEnd)

	data, err := os.ReadFile(note)
	if err != nil {
		return err
	}
	all := lines(string(data))
	var out []string
	hasStart := false
	for _, l := range all {
		hasStart = hasStart || l == blockStart
	}
	if hasStart {
		skip := false
		for _, l := range all {
			switch {
			case l == blockStart:
				out = append(out, block...)
				skip = true
			case skip && l == blockEnd:
				skip = false
			case !skip:
				out = append(out, l)
			}
		}
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
	return os.WriteFile(note, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
