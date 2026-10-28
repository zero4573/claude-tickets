package assets

import (
	"strings"
	"testing"
)

func retroText(t *testing.T, path string) string {
	t.Helper()
	b, err := Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

// Workflow rules learned from a retro stay in the skills.
func TestRetroRulesPinned(t *testing.T) {
	cases := map[string][]string{
		"plugin/skills/lead/SKILL.md": {
			"`ct ws diff-check -C <ws>/<slug> --ticket <ID> HEAD..task/<ID>-NN-<task-slug>`",
			"exit 3 → don't merge",
			"NO REPORT",
			"get `</dev/null`",
			"The report's `Commits:` lists shas and its `Checks:` full run",
			"`db: heavy`",
			"non-urgent entries arrive batched with its wave line",
			"Small waves get no reviewer",
			"a `vault-id` hit on a line kept from the base, or in a fixture that models a ticket, is noted on the board line",
			"Ask user: none | R-9093:",
			"record the pass in `plan.md ## Decisions`",
			"Task subjects off the commit style",
		},
		"plugin/skills/work-ticket/SKILL.md": {
			"`NO REPORT: NN, …`",
			"`orphaned`",
			"send the lead only the next wave line",
		},
		"plugin/skills/task-agent/SKILL.md": {
			"never \"see git log\"",
			"no subsets",
			"`REPORT NOT WRITTEN: <reason>`",
			"follows the commit style",
			"get `</dev/null`",
			"Commits: <sha> <subject>",
			"`ct ws diff-check -C <worktree> --ticket <ID> <ticket-branch>..HEAD` exits 0",
			"`board.md` `commit-style:` wins",
			"Checks: <the repo's full check command>",
			"except a hit on a line kept from the base or in a fixture that models a ticket: note that one in the report",
		},
		"plugin/skills/wave-review/SKILL.md": {
			"`conventions.md ## Test DB`",
			"any skipped DB test is a must-fix (M) finding",
			"except a hit on a line kept from the base or in a fixture that models a ticket: note it, don't must-fix it",
		},
	}
	for path, want := range cases {
		text := retroText(t, path)
		for _, s := range want {
			if !strings.Contains(text, s) {
				t.Errorf("%s: missing %q", path, s)
			}
		}
	}
}
