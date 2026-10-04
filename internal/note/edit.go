package note

import (
	"os"
	"regexp"
	"strings"
)

// fileLines reads a file as lines (no line endings; a missing final
// newline is ignored).
func fileLines(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" && len(data) <= 1 {
		if len(data) == 0 {
			return nil, nil
		}
		return []string{""}, nil
	}
	return strings.Split(s, "\n"), nil
}

func writeLines(file string, lines []string) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(file, []byte(b.String()), 0o644)
}

// Set sets a frontmatter field to value, written as given (every line of
// the field is replaced; a missing field is added at the end of the
// frontmatter). An empty value leaves "key:". Notes without frontmatter
// are left alone.
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
	done, in := false, true
	for _, l := range lines[1:] {
		switch {
		case in && l == "---":
			if !done {
				out = append(out, field)
			}
			in, done = false, true
			out = append(out, l)
		case in && strings.HasPrefix(l, key+":"):
			out = append(out, field)
			done = true
		default:
			out = append(out, l)
		}
	}
	return writeLines(file, out)
}

var flowTags = regexp.MustCompile(`^tags: *\[(.*)\] *$`)

// EditTags adds or removes a tag in a flow-style "tags: [...]" list.
func EditTags(file string, add bool, tag string) error {
	lines, err := fileLines(file)
	if err != nil {
		return err
	}
	if len(lines) == 0 || lines[0] != "---" {
		return nil
	}
	for i := 1; i < len(lines) && lines[i] != "---"; i++ {
		m := flowTags.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		var out []string
		found := false
		for _, x := range strings.Split(m[1], ",") {
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
		lines[i] = "tags: [" + strings.Join(out, ", ") + "]"
	}
	return writeLines(file, lines)
}

// Fields is a note's frontmatter as the sync's index reads it: every
// "key:" line (lower-case keys), the last one winning, values trimmed and
// stripped of a leading and a trailing double quote.
func Fields(file string) map[string]string {
	lines, _ := fileLines(file)
	f := map[string]string{}
	if len(lines) == 0 || lines[0] != "---" {
		return f
	}
	for _, l := range lines[1:] {
		if l == "---" {
			break
		}
		m := fieldRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v := strings.Trim(m[2], " \t")
		v = strings.TrimPrefix(v, `"`)
		v = strings.TrimSuffix(v, `"`)
		f[m[1]] = v
	}
	return f
}

var fieldRe = regexp.MustCompile(`^([a-z][a-z-]*):(.*)$`)
