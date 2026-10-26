package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// The commit rules in the skills and the vault scaffold: vault-only IDs stay
// in the vault, commit subjects follow the ticket's commit style, merge
// commits and the resume proof carry no task numbers, and the hand-over and
// save skills use the docs and tidy marks.
func TestCommitRulesInSkills(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
	scaffold := func(p string) string {
		data, err := fs.ReadFile(Scaffold, p)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
	for name, c := range map[string]struct {
		text string
		want []string
	}{
		"task-agent":    {read("plugin/skills/task-agent/SKILL.md"), []string{"subject in the ticket's commit style", "Vault-only IDs", "never go into code, comments, test names, docs or commit messages"}},
		"lead":          {read("plugin/skills/lead/SKILL.md"), []string{"-m \"<subject>\"", "`commit-style:`", "never `merge task NN`", "`%P` gives full hashes", "second parent is the task branch tip", "rev-parse -q --verify task/<ID>-NN-<task-slug>", "in the ticket's commit style"}},
		"lead-handover": {read("plugin/skills/lead/handover.md"), []string{"subject in the ticket's commit style", "ct ws marks <ID>", "B: tidy only", "C: neither", "`tickets:docs`", "`tickets:tidy`"}},
		"work-ticket":   {read("plugin/skills/work-ticket/SKILL.md"), []string{"`commit-style:`", "vault-only IDs"}},
		"kickoff":       {read("plugin/skills/work-ticket/kickoff.md"), []string{"AskUserQuestion", "`repo` (recommended", "`conventional`", "commit-style: ticket", "`feature/<description>`", "always pass"}},
		"po-handover":   {read("plugin/skills/work-ticket/handover.md"), []string{"docs, tidy", "/tickets:save", "ct ws sign <ID>", "--skip-marks"}},
		"wave-review":   {read("plugin/skills/wave-review/SKILL.md"), []string{"Vault-only IDs", "rg '^\\+'", "A hit is a must-fix"}},
		"pr-feedback":   {read("plugin/skills/pr-feedback/SKILL.md"), []string{"commit style", "never carry vault-only IDs"}},
		"save":          {read("plugin/skills/save/SKILL.md"), []string{"ct ws marks <ID>", "exit 3", "/tickets:docs", "/tickets:tidy", "feature/<description>"}},
	} {
		for _, s := range c.want {
			if !strings.Contains(c.text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}
	agents := scaffold("vault-scaffold/AGENTS.md")
	for _, s := range []string{"### Vault-only IDs", "never in code, comments, test names, docs, commit messages", "(`PROJ-12`) may appear there", "`commit-style:`"} {
		if !strings.Contains(agents, s) {
			t.Errorf("vault AGENTS.md: missing %q", s)
		}
	}
	if board := scaffold("vault-scaffold/templates/board.md"); !strings.Contains(board, `commit-style: ""`) {
		t.Error("board template: no commit-style field")
	}
	if lead := read("plugin/skills/lead/SKILL.md"); strings.Contains(lead, "merge task NN:") {
		t.Error("lead: still names the task number in merge subjects")
	}
}
