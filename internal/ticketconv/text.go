// Package ticketconv converts a manual ticket into one filed at a ticket
// source (ct ticket convert): the text rewrites on the ticket's vault
// folder and workspace.json live here, the command's order of steps in Run.
package ticketconv

import (
	"regexp"
	"strings"
)

func wordChar(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}

// RewriteID replaces the whole-word ticket ID old with new in text. A match
// is whole when no letter, digit, "_" or "-" comes before it and no letter,
// digit or "_" after it, so MAN-4 is found in "MAN-4-status" and "[[MAN-4]]"
// but not in MAN-40 or XMAN-4. Names of task branches (task/<old>-...) and
// the lines of the fields keep (branch: and converted-from:) are left alone:
// the branches keep their names.
func RewriteID(text, old, new string) string {
	if old == "" || !strings.Contains(text, old) {
		return text
	}
	lines := strings.SplitAfter(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "branch:") || strings.HasPrefix(l, "converted-from:") {
			continue
		}
		lines[i] = rewriteLine(l, old, new)
	}
	return strings.Join(lines, "")
}

func rewriteLine(l, old, new string) string {
	var out strings.Builder
	i := 0
	for {
		j := strings.Index(l[i:], old)
		if j < 0 {
			out.WriteString(l[i:])
			return out.String()
		}
		j += i
		end := j + len(old)
		whole := (j == 0 || !(wordChar(l[j-1]) || l[j-1] == '-')) && (end == len(l) || !wordChar(l[end]))
		if whole && strings.HasSuffix(l[:j], "task/") && end < len(l) && l[end] == '-' {
			whole = false
		}
		out.WriteString(l[i:j])
		if whole {
			out.WriteString(new)
		} else {
			out.WriteString(old)
		}
		i = end
	}
}

// RenameFile is the new base name of a file of the ticket's folder: the old
// ID at the start of the name, followed by "." or "-", becomes the new one.
func RenameFile(old, new string) func(string) string {
	return func(base string) string {
		if rest, ok := strings.CutPrefix(base, old); ok && rest != "" && (rest[0] == '.' || rest[0] == '-') {
			return new + rest
		}
		return base
	}
}

// Section is the body of the note's "## <heading>" section, up to the next
// "## " heading, trimmed, with HTML comments left out.
func Section(text, heading string) string {
	var out []string
	in := false
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "## ") {
			in = strings.TrimSpace(l[3:]) == heading
			continue
		}
		if in {
			out = append(out, strings.TrimRight(l, "\r"))
		}
	}
	return strings.TrimSpace(stripComments(strings.Join(out, "\n")))
}

var commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

func stripComments(s string) string { return commentRe.ReplaceAllString(s, "") }

var taskRe = regexp.MustCompile(`(?m)^(\s*(?:[-*+]|\d+[.)])\s+)\[([ xX]?)\]\s`)

// HasTaskList is whether text has a task list item (- [ ] or - [x]).
func HasTaskList(text string) bool { return taskRe.MatchString(text) }

// plainTasks rewrites task list items to plain list items that keep the
// state as text (`- \[ \] item`), the form ct sync writes for them: the
// source's markdown shows a real checkbox as a bare "[]".
func plainTasks(md string) string {
	return taskRe.ReplaceAllStringFunc(md, func(m string) string {
		sub := taskRe.FindStringSubmatch(m)
		box := `\[ \] `
		if sub[2] == "x" || sub[2] == "X" {
			box = `\[x\] `
		}
		return sub[1] + box
	})
}

// Description is what the source's issue gets as its description: the
// note's "Description" and "Acceptance criteria" sections as markdown.
func Description(text string) string {
	var parts []string
	if d := Section(text, "Description"); d != "" {
		parts = append(parts, d)
	}
	if a := Section(text, "Acceptance criteria"); a != "" {
		parts = append(parts, "### Acceptance criteria\n"+a)
	}
	return plainTasks(strings.Join(parts, "\n\n"))
}
