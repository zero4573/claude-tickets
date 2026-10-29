package ticketconv

import (
	"strings"
)

// frontmatter splits a note into its frontmatter lines (without the
// delimiters) and the rest; ok is false for a note without frontmatter.
func frontmatter(text string) (fm []string, rest string, ok bool) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return nil, text, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			return lines[1:i], strings.Join(lines[i:], ""), true
		}
	}
	return nil, text, false
}

// setKey sets key (value as written; empty drops the field) in frontmatter
// lines, replacing its continuation lines too, else adding it at the end.
func setKey(fm []string, key, value string, drop bool) []string {
	var out []string
	done, skipping := false, false
	for _, l := range fm {
		if skipping && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(strings.TrimSpace(l), "- ")) {
			continue
		}
		skipping = false
		if strings.HasPrefix(l, key+":") {
			skipping = true
			if drop || done {
				continue
			}
			out = append(out, key+": "+value+"\n")
			done = true
			continue
		}
		out = append(out, l)
	}
	if !done && !drop {
		if n := len(out); n > 0 && !strings.HasSuffix(out[n-1], "\n") {
			out[n-1] += "\n"
		}
		out = append(out, key+": "+value+"\n")
	}
	return out
}

// getKey is the scalar value of key in frontmatter lines, unquoted.
func getKey(fm []string, key string) string {
	for _, l := range fm {
		if v, ok := strings.CutPrefix(l, key+":"); ok {
			v = strings.TrimSpace(v)
			if n := len(v); n >= 2 && (v[0] == '"' && v[n-1] == '"' || v[0] == '\'' && v[n-1] == '\'') {
				v = v[1 : n-1]
			}
			return v
		}
	}
	return ""
}

// Field is the scalar frontmatter value of key in a note's text.
func Field(text, key string) string {
	fm, _, _ := frontmatter(text)
	return getKey(fm, key)
}

// SetFields sets frontmatter fields (a value of "" drops the field), keeping
// the order of the existing ones; a note without frontmatter is returned
// as it is.
func SetFields(text string, kv ...string) string {
	fm, rest, ok := frontmatter(text)
	if !ok {
		return text
	}
	for i := 0; i+1 < len(kv); i += 2 {
		fm = setKey(fm, kv[i], kv[i+1], kv[i+1] == "")
	}
	return "---\n" + strings.Join(fm, "") + rest
}

// editTags drops and adds tags in a flow-style "tags: [a, b]" line (a
// block-style list is left alone).
func editTags(fm []string, drop string, add ...string) []string {
	for i, l := range fm {
		v, ok := strings.CutPrefix(l, "tags:")
		v = strings.TrimSpace(v)
		if !ok || !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
			continue
		}
		var tags []string
		for _, t := range strings.Split(v[1:len(v)-1], ",") {
			if t = strings.TrimSpace(t); t != "" && t != drop {
				tags = append(tags, t)
			}
		}
	next:
		for _, a := range add {
			for _, t := range tags {
				if t == a {
					continue next
				}
			}
			tags = append(tags, a)
		}
		fm[i] = "tags: [" + strings.Join(tags, ", ") + "]\n"
	}
	return fm
}

const (
	blockStart = "<!-- source:start -->"
	blockEnd   = "<!-- source:end -->"
)

// Flip turns a manual ticket's note text into a synced one's: source,
// source-id and source-url, the manual tag swapped for the source's (and the
// project's), the converted-from record of the old ID (dropped by the last
// step of a convert), the convert-* records of a half-done run dropped, and
// an empty source block after the title heading for ct sync to fill. It is
// stable: flipping a flipped note changes nothing.
func Flip(text, source, id, url, oldID, label string) string {
	fm, rest, ok := frontmatter(text)
	if !ok {
		return text
	}
	for _, kv := range [][2]string{{"source", source}, {"source-id", id}, {"source-url", url}, {"converted-from", oldID}} {
		fm = setKey(fm, kv[0], kv[1], false)
	}
	for _, k := range []string{"convert-source", "convert-key", "convert-url"} {
		fm = setKey(fm, k, "", true)
	}
	project := strings.ToLower(strings.SplitN(id, "-", 2)[0])
	fm = editTags(fm, "manual", source, project)
	if !strings.Contains(rest, blockStart) {
		block := blockStart + "\n## Source (" + label + ")\n" + blockEnd + "\n"
		lines := strings.SplitAfter(rest, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "# ") {
				lines = append(lines[:i+1], append([]string{"\n" + block}, lines[i+1:]...)...)
				rest = strings.Join(lines, "")
				break
			}
		}
	}
	return "---\n" + strings.Join(fm, "") + rest
}

// Label is a source's name as shown in the source block: jira -> Jira.
func Label(source string) string {
	if source == "" {
		return source
	}
	return strings.ToUpper(source[:1]) + source[1:]
}
