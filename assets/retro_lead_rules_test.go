package assets

import (
	"strings"
	"testing"
)

func retroLeadRead(t *testing.T, path string) string {
	t.Helper()
	b, err := Files.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

func retroLeadWant(t *testing.T, path string, want ...string) {
	t.Helper()
	text := retroLeadRead(t, path)
	for _, s := range want {
		if !strings.Contains(text, s) {
			t.Errorf("%s: missing %q", path, s)
		}
	}
}

func retroLeadNot(t *testing.T, path string, banned ...string) {
	t.Helper()
	text := retroLeadRead(t, path)
	for _, s := range banned {
		if strings.Contains(text, s) {
			t.Errorf("%s: still has %q", path, s)
		}
	}
}

func TestRetroLeadCrashWindowContract(t *testing.T) {
	retroLeadWant(t, "plugin/skills/lead/SKILL.md",
		"writes several files or trees as one step",
		"crash-window table (each step, what a crash there leaves, how a rerun finishes it)",
		"before the first build task")
	retroLeadWant(t, "plugin/skills/lead/reference.md", "## Crash-window contract")
}

func TestRetroLeadFollowUpDecision(t *testing.T) {
	retroLeadWant(t, "plugin/skills/lead/SKILL.md",
		"changes behaviour names the decision it rests on in `plan.md ## Decisions`",
		"goes beyond that decision's scope it is a task plus the wave review")
}

func TestRetroLeadDiffCheckNotes(t *testing.T) {
	retroLeadWant(t, "plugin/skills/lead/SKILL.md",
		"`note` lines (contract `diff-check-notes-v1`) never block",
		"`note test-changed`", "possible fix-round regression",
		"`note unsigned`", "never rewrite task commits",
		"\"unsigned, re-sign on the host\" to the board line")
	retroLeadNot(t, "plugin/skills/lead/SKILL.md", "rebase -q --exec", "--amend --no-edit -S")
}

func TestRetroLeadContractReviewLine(t *testing.T) {
	retroLeadWant(t, "plugin/skills/lead/SKILL.md",
		"- w<N> contract · <contract file> · - · M/S/C <m>/<s>/<c> · [[<review>]] · <how fixed>")
}

func TestRetroLeadTaskAgentSigns(t *testing.T) {
	retroLeadWant(t, "plugin/skills/task-agent/SKILL.md",
		"Commit signed (`git commit -S`)",
		"`--no-gpg-sign` only after a signed commit failed", "under `Checks:`")
}
