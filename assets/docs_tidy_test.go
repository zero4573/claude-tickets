package assets

import (
	"io/fs"
	"strings"
	"testing"
)

func TestDocsTidySkills(t *testing.T) {
	const order = "docs → tidy → `/tickets:save` → `ct ws sign`"
	want := map[string][]string{
		"docs": {"ct ws mark <ID> docs", "sequenceDiagram", "flowchart", "## Documentation", "without `;` or `#`", order},
		"tidy": {"ct ws mark <ID> tidy", "^{tree}", "update-ref", "--not --remotes",
			"immutable_heads()", "jj absorb --from", "jj abandon", "jj git clone --colocate",
			"--boundary", "--is-ancestor", "--allow-backwards", "ct ws tidy-clean <ID>", "tasks/tidy-<slug>", order},
	}
	for name, strs := range want {
		b, err := fs.ReadFile(Files, "plugin/skills/"+name+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		if n := strings.Count(text, "\n"); n > 100 {
			t.Errorf("%s: %d lines, cap 100", name, n)
		}
		head, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
		if !ok {
			t.Fatalf("%s: no frontmatter", name)
		}
		for _, f := range []string{"name: " + name + "\n", "description: ", "argument-hint: "} {
			if !strings.Contains(head+"\n", f) {
				t.Errorf("%s: frontmatter lacks %q", name, f)
			}
		}
		if strings.Contains(head, "disable-model-invocation") {
			t.Errorf("%s: must be model-invocable", name)
		}
		for _, s := range append(strs, "Started by the PO", "The lead's part", "The task agent's part") {
			if !strings.Contains(text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}
}

func TestTidyChecksAncestryBeforeUpdateRef(t *testing.T) {
	b, err := fs.ReadFile(Files, "plugin/skills/tidy/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	anc := strings.Index(text, "--is-ancestor")
	upd := strings.Index(text, "git update-ref refs/tidy-backup")
	if anc < 0 || upd < 0 || anc > upd {
		t.Errorf("the --is-ancestor check (%d) must come before update-ref (%d)", anc, upd)
	}
}

func TestPRFeedbackSignsWithSkipMarks(t *testing.T) {
	b, err := fs.ReadFile(Files, "plugin/skills/pr-feedback/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "ct ws sign <ID> --skip-marks"); n < 2 {
		t.Errorf("pr-feedback names ct ws sign <ID> --skip-marks %d times, want both places", n)
	}
	if strings.Contains(strings.ReplaceAll(string(b), "ct ws sign <ID> --skip-marks", ""), "ct ws sign <ID>") {
		t.Error("pr-feedback still has a bare ct ws sign <ID>")
	}
}

func TestTidyCleanNamed(t *testing.T) {
	b, err := fs.ReadFile(Files, "plugin/skills/tidy/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "rm -rf` is denied, give") {
		t.Error("tidy still tells the user to delete the clone by hand")
	}
	dir, cmd := strings.Index(text, "tasks/tidy-<slug>"), strings.Index(text, "ct ws tidy-clean <ID>")
	if dir < 0 || cmd < 0 {
		t.Fatal("tidy must name the clone dir and the command that deletes it")
	}
	if dir > cmd {
		t.Error("the clone dir must be named before the command that deletes it")
	}
}
