package assets

import (
	"io/fs"
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

// Retro findings about tix itself become tasks in the vault's
// tix-improvements/, never backlog items or tickets
func TestRetroTixImprovements(t *testing.T) {
	for _, p := range []string{"plugin/skills/retro/SKILL.md", "plugin/skills/lead/handover.md"} {
		retroLeadWant(t, p, "`tix-improvements/`", "the vault's `AGENTS.md`, \"Vault Structure\"")
		retroLeadNot(t, p, "<repo|plugin|vault>")
	}
	retroLeadWant(t, "plugin/skills/retro/SKILL.md",
		"**Kept `tix` candidates** never become a backlog item or a ticket")
	retroLeadWant(t, "plugin/skills/lead/handover.md",
		"written now with no question and named in `## Review`: never a backlog item or a follow-up ticket")
	data, err := fs.ReadFile(Scaffold, "vault-scaffold/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	agents := strings.Join(strings.Fields(string(data)), " ")
	for _, s := range []string{
		"├── tix-improvements/",
		"`tix-improvements/<yyyy-MM-dd>-<HHmm>-<ID>-retro.md`",
		"`type: tix-improvements`",
		"**Exception, retros:**",
		"the notes in `tix-improvements/`",
	} {
		if !strings.Contains(agents, s) {
			t.Errorf("vault-scaffold/AGENTS.md: missing %q", s)
		}
	}
}
