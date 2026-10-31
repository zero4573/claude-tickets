package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunningAgents(t *testing.T) {
	for name, tc := range map[string]struct {
		board string
		want  bool
	}{
		"empty":       {"---\npo:\n  agents: []\n  finished: []\n---\n", false},
		"inline":      {"---\npo:\n  agents: [\"a2 · tix:task · w1\"]\n---\n", true},
		"list":        {"---\npo:\n  agents:\n    - \"a2 · tix:task · w1\"\n  finished: []\n---\n", true},
		"list ended":  {"---\npo:\n  agents:\n  finished:\n    - \"w1 · task 01 · done\"\n---\n", false},
		"other block": {"---\nlead:\n  agents:\n    - x\n---\n", false},
		"body only":   {"---\ntype: board\n---\npo:\n  agents:\n    - x\n", false},
	} {
		f := filepath.Join(t.TempDir(), "board.md")
		if err := os.WriteFile(f, []byte(tc.board), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := runningAgents(f); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
	if runningAgents(filepath.Join(t.TempDir(), "missing.md")) {
		t.Error("missing board: no agents")
	}
}
