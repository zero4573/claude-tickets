package links

import "strings"

// Targets is the bare wikilink targets of a note's text, in order of first
// appearance and without duplicates: "[[a/b.md#h|x]]" gives "b". Code spans
// and fenced code are skipped; frontmatter links count.
func Targets(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range linksIn("", text) {
		t := BareName(target(l.parts))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// BareName is a link target or note path reduced to its note name: the last
// path element without a ".md" suffix.
func BareName(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndexAny(t, `/\`); i >= 0 {
		t = t[i+1:]
	}
	return strings.TrimSuffix(t, ".md")
}
