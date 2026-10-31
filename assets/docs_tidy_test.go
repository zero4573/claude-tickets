package assets

import (
	"io/fs"
	"strings"
	"testing"
)

func TestTidySkill(t *testing.T) {
	const order = "tidy → `/tix:save` (which pushes)"
	want := map[string][]string{
		"tidy": {"tix ws mark <ID> tidy", "^{tree}", "update-ref", "--not --remotes",
			"immutable_heads()", "jj absorb --from", "jj abandon", "jj git clone --colocate",
			"--boundary", "--is-ancestor", "--allow-backwards", "tix ws tidy-clean <ID>", "tasks/tidy-<slug>", order},
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

func TestPRFeedbackPushesThroughSave(t *testing.T) {
	b, err := fs.ReadFile(Files, "plugin/skills/pr-feedback/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if n := strings.Count(text, "/tix:save"); n < 2 {
		t.Errorf("pr-feedback names /tix:save %d times, want the rules and the closing message", n)
	}
	if !strings.Contains(text, "anyway at its marks gate") {
		t.Error("pr-feedback must say to push anyway at save's marks gate (feedback makes the marks stale)")
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
	dir, cmd := strings.Index(text, "tasks/tidy-<slug>"), strings.Index(text, "tix ws tidy-clean <ID>")
	if dir < 0 || cmd < 0 {
		t.Fatal("tidy must name the clone dir and the command that deletes it")
	}
	if dir > cmd {
		t.Error("the clone dir must be named before the command that deletes it")
	}
}

func TestSavePushesLast(t *testing.T) {
	b, err := fs.ReadFile(Files, "plugin/skills/save/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	push := strings.Index(text, "### 8. Push the ticket branches")
	release := strings.Index(text, "Run `tix vault lock release <vault> <ID>`")
	if push < 0 || release < 0 || push < release {
		t.Fatalf("save: the push step (%d) must come after the lock release (%d)", push, release)
	}
	step := text[push:]
	for _, s := range []string{"tix ws marks <ID>", "save anyway", "AskUserQuestion", "%G?", "--not --remotes",
		"push -u <remote> <branch>", "branch.<branch>.remote", "`tasks/*`", "--force", "--delete",
		"rebase --exec 'git commit --amend --no-edit -S'", "Rejected", "the exact command"} {
		if !strings.Contains(step, s) {
			t.Errorf("save push step: missing %q", s)
		}
	}
}

func TestNoWsSign(t *testing.T) {
	err := fs.WalkDir(Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(Files, p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "ws sign") || strings.Contains(string(b), "--skip-marks") {
			t.Errorf("%s still names tix ws sign (removed: sessions sign, /tix:save pushes)", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
