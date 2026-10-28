package jira

import (
	"regexp"
	"strings"
)

// taskItem matches a task list item as Jira's markdown may give it: any
// blockquote markers, a bullet or numbered list marker, then an empty, ticked
// or unticked box and the item text. A box with no marker counts only as
// Jira's empty "[]", so prose such as "[x] marks the spot" is left alone.
var taskItem = regexp.MustCompile(`^(\s*(?:>\s*)*)([-*+]|\d+[.)])?\s*\[([ xX]?)\]\s+(.*)$`)

// fenceOpen matches the opening line of a fenced code block.
var fenceOpen = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// plainTaskItems rewrites task list items to plain list items with the state
// as text, `- \[ \] item` and `- \[x\] item`: the vault allows checkboxes only
// in work sections, and the Tasks views must not list Jira items. Indentation,
// blockquote markers and numbers are kept, fenced code blocks are left alone,
// and the result is stable.
func plainTaskItems(md string) string {
	lines := strings.Split(md, "\n")
	fence := ""
	for i, l := range lines {
		if fence != "" {
			// A closing fence: the same character, at least as long, nothing else
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(l); m != nil {
			fence = m[1]
			continue
		}
		m := taskItem.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		prefix, marker, box, text := m[1], m[2], m[3], m[4]
		if marker == "" && box != "" {
			continue
		}
		if marker == "" || strings.ContainsAny(marker, "*+") {
			marker = "-"
		}
		state := " "
		if box != " " && box != "" {
			state = "x"
		}
		lines[i] = prefix + marker + ` \[` + state + `\] ` + text
	}
	return strings.Join(lines, "\n")
}
