package vindex

import (
	"encoding/json"
	"regexp"
	"strings"
)

var wikiRe = regexp.MustCompile(`!?\[\[([^\[\]|]*)(?:\|([^\[\]]*))?\]\]`)

// cleanLinks writes [[a|b]] as b and [[a]] / ![[a]] as a, for the embed
// input and the BM25 text (chunk.Text itself keeps the targets as they are;
// the cleaning is done here, not in package chunk).
func cleanLinks(s string) string {
	if !strings.Contains(s, "[[") {
		return s
	}
	return wikiRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikiRe.FindStringSubmatch(m)
		if strings.Contains(m, "|") {
			return strings.TrimSpace(sub[2])
		}
		return strings.TrimSpace(sub[1])
	})
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
