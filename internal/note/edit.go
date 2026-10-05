package note

import (
	"os"
	"regexp"
	"strings"
)

// Normalize drops a UTF-8 byte order mark and turns CRLF line endings into
// LF (Windows checkouts, other editors): every reader of notes works on
// that, so a note's frontmatter and markers are found either way. Edits
// write the note back with LF.
func Normalize(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// fileLines ignores a missing final newline.
func fileLines(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	s := Normalize(string(data))
	if s == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n"), nil
}

func writeLines(file string, lines []string) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(file, []byte(b.String()), 0o644)
}

// continuation reports whether a frontmatter line belongs to the key above
// it: an indented line, or a block-style list item (Obsidian's Properties
// editor writes lists as "key:\n  - item").
func continuation(l string) bool {
	return strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "- ") || l == "-"
}

// Set writes value as given, replacing every line of the field, including a
// block-style list's items; a missing field is added at the end of the
// frontmatter. An empty value leaves "key:". Notes without frontmatter are
// left alone.
func Set(file, key, value string) error {
	lines, err := fileLines(file)
	if err != nil {
		return err
	}
	if len(lines) == 0 || lines[0] != "---" {
		return nil
	}
	field := key + ":"
	if value != "" {
		field += " " + value
	}
	out := []string{lines[0]}
	done, in, dropping := false, true, false
	for _, l := range lines[1:] {
		if dropping && in && continuation(l) {
			continue
		}
		dropping = false
		switch {
		case in && l == "---":
			if !done {
				out = append(out, field)
			}
			in, done = false, true
			out = append(out, l)
		case in && strings.HasPrefix(l, key+":"):
			out = append(out, field)
			done, dropping = true, true
		default:
			out = append(out, l)
		}
	}
	return writeLines(file, out)
}

var (
	flowTags  = regexp.MustCompile(`^tags: *\[(.*)\] *$`)
	blockTags = regexp.MustCompile(`^tags: *$`)
)

// listItem is a block-style list item's value, unquoted.
func listItem(l string) string {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "-"))
	if n := len(v); n >= 2 && (v[0] == '"' && v[n-1] == '"' || v[0] == '\'' && v[n-1] == '\'') {
		v = v[1 : n-1]
	}
	return v
}

// EditTags adds or removes a tag. A block-style tags list is rewritten in
// flow style ("tags: [a, b]").
func EditTags(file string, add bool, tag string) error {
	lines, err := fileLines(file)
	if err != nil {
		return err
	}
	if len(lines) == 0 || lines[0] != "---" {
		return nil
	}
	for i := 1; i < len(lines) && lines[i] != "---"; i++ {
		var items []string
		end := i + 1
		if m := flowTags.FindStringSubmatch(lines[i]); m != nil {
			items = strings.Split(m[1], ",")
		} else if blockTags.MatchString(lines[i]) {
			for end < len(lines) && continuation(lines[end]) {
				items = append(items, listItem(lines[end]))
				end++
			}
		} else {
			continue
		}
		var out []string
		found := false
		for _, x := range items {
			x = strings.Trim(x, " ")
			if x == "" {
				continue
			}
			if x == tag {
				found = true
				if !add {
					continue
				}
			}
			out = append(out, x)
		}
		if add && !found {
			out = append(out, tag)
		}
		lines = append(append(lines[:i:i], "tags: ["+strings.Join(out, ", ")+"]"), lines[end:]...)
	}
	return writeLines(file, lines)
}

// Fields is a note's frontmatter as the sync's index reads it: every
// "key:" line (lower-case keys), the last one winning, values trimmed and
// stripped of a leading and a trailing double quote. A block-style list
// reads as its flow form ("[a, b]").
func Fields(file string) map[string]string {
	lines, _ := fileLines(file)
	f := map[string]string{}
	if len(lines) == 0 || lines[0] != "---" {
		return f
	}
	for i := 1; i < len(lines) && lines[i] != "---"; i++ {
		m := fieldRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		v := strings.Trim(m[2], " \t")
		if v == "" && i+1 < len(lines) && continuation(lines[i+1]) {
			var items []string
			for i+1 < len(lines) && continuation(lines[i+1]) {
				i++
				items = append(items, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "-")))
			}
			v = "[" + strings.Join(items, ", ") + "]"
		}
		v = strings.TrimPrefix(v, `"`)
		v = strings.TrimSuffix(v, `"`)
		f[m[1]] = v
	}
	return f
}

var fieldRe = regexp.MustCompile(`^([a-z][a-z-]*):(.*)$`)
