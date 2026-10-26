package assets

import (
	"strings"
	"testing"
)

// The lead gives concurrent tasks in one Go package distinct name prefixes,
// for their Test functions as well as their helpers: two tasks once declared
// the same Test function and the clash only showed at merge.
func TestLeadPrefixConcurrentTestNames(t *testing.T) {
	b, err := Files.ReadFile("plugin/skills/lead/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(b)), " ")
	for _, s := range []string{
		"Concurrent tasks in one Go package get distinct name prefixes",
		"for their helpers and `Test` functions",
	} {
		if !strings.Contains(text, s) {
			t.Errorf("lead/SKILL.md: missing %q", s)
		}
	}
}
