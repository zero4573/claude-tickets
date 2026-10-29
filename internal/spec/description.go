package spec

import "strings"

// changeBounds finds the "## Spec change" section of a description as
// line indexes: from is its heading, to the next "#" or "##" heading (or
// len(lines)). Lines keep their line endings. from is -1 when the heading
// is missing (a renamed heading counts as missing).
func changeBounds(lines []string) (from, to int) {
	from, to = -1, len(lines)
	fence := false
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "\r\n")
		if isFence(ln) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		lvl, h := heading(ln)
		if from < 0 {
			if lvl == 2 && h == SectionHeading {
				from = i
			}
		} else if lvl == 1 || lvl == 2 {
			return from, i
		}
	}
	return from, to
}

func keepLines(s string) []string {
	return strings.SplitAfter(s, "\n")
}

// CurrentChange returns the description's "## Spec change" section as
// written (heading included), and whether there is one.
func CurrentChange(desc string) (string, bool) {
	lines := keepLines(desc)
	from, to := changeBounds(lines)
	if from < 0 {
		return "", false
	}
	return strings.Join(lines[from:to], ""), true
}

// HasChange reports whether the description's "## Spec change" section
// holds anything beyond the "No spec change." placeholder.
func HasChange(desc string) bool {
	lines := keepLines(desc)
	from, to := changeBounds(lines)
	if from < 0 {
		return false
	}
	body := strings.TrimSpace(strings.Join(lines[from+1:to], ""))
	return body != "" && body != "No spec change."
}

// ReplaceChange puts the spec change section, built from RenderChange's
// output (body; "" for no change), into a ticket description: it replaces
// the section from a "## Spec change" heading to the next "#" or "##"
// heading, or appends one when there is none. Everything else stays byte
// for byte; the new section uses the description's line endings.
func ReplaceChange(desc, body string) string {
	eol := "\n"
	if strings.Contains(desc, "\r\n") {
		eol = "\r\n"
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		body = "No spec change."
	}
	section := "## " + SectionHeading + "\n\n" + body + "\n"
	if eol != "\n" {
		section = strings.ReplaceAll(section, "\n", eol)
	}
	lines := keepLines(desc)
	from, to := changeBounds(lines)
	if from < 0 {
		if desc == "" {
			return section
		}
		sep := ""
		switch {
		case !strings.HasSuffix(desc, "\n"):
			sep = eol + eol
		case !strings.HasSuffix(desc, eol+eol) && !strings.HasSuffix(desc, "\n\n"):
			sep = eol
		}
		return desc + sep + section
	}
	if to < len(lines) {
		section += eol
	}
	return strings.Join(lines[:from], "") + section + strings.Join(lines[to:], "")
}
