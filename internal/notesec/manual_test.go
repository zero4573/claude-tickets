package notesec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noteVault(t *testing.T, id, fm, body string) (string, string) {
	t.Helper()
	v := t.TempDir()
	os.MkdirAll(filepath.Join(v, ".obsidian"), 0o755)
	os.MkdirAll(filepath.Join(v, "tickets", id), 0o755)
	f := filepath.Join(v, "tickets", id, id+".md")
	if err := os.WriteFile(f, []byte("---\ntitle: T\n"+fm+"status: open\n---\n# T\n\n## Description\n\nd\n\n## Acceptance criteria\n\na\n\n## Context / links\n\nc\n\n## Tasks\n\nt\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return v, "tickets/" + id + "/" + id + ".md"
}

func write(c Ctx, rel string, op Op) error {
	_, err := Apply(c, rel, "o", op)
	return err
}

func TestManualContentAllowed(t *testing.T) {
	for _, c := range []func(string) Ctx{
		func(v string) Ctx { return Ctx{Vault: v, Workspace: "P-1"} },
		func(v string) Ctx { return Ctx{Vault: v, Save: true} },
	} {
		for _, sec := range []string{"Description", "Acceptance criteria", "Context / links"} {
			v, rel := noteVault(t, "P-1", "source: manual\n", "")
			cx := c(v)
			if err := write(cx, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, sec, []byte("new\n"), false) }); err != nil {
				t.Errorf("set %s: %v", sec, err)
			}
			if err := write(cx, rel, func(s []byte) ([]byte, Target, error) { return Append(s, sec, []byte("more\n"), 0) }); err != nil {
				t.Errorf("append %s: %v", sec, err)
			}
			if b, _ := os.ReadFile(filepath.Join(v, rel)); !strings.Contains(string(b), "new\nmore") {
				t.Errorf("%s not written: %q", sec, b)
			}
		}
	}
	v, rel := noteVault(t, "P-1", "source:  Manual \n", "")
	c := Ctx{Vault: v, Workspace: "P-1"}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return Append(s, "Extra", []byte("x\n"), 2) }); err != nil {
		t.Error(err)
	}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
		return Append(s, "Description", []byte("### Sub\n\nx\n"), 3)
	}); err != nil {
		t.Error(err)
	}
	// an H1 rewrite with subsections that edits Description
	b, _ := os.ReadFile(filepath.Join(v, rel))
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
		return SetSection(s, "T", []byte(strings.Replace(string(b[strings.Index(string(b), "# T\n")+4:]), "\nd\n", "\nedited\n", 1)), true)
	}); err != nil {
		t.Errorf("H1 rewrite: %v", err)
	}
	// a second Description in the rewrite is refused
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
		return SetSection(s, "T", []byte("\n## Description\n\nx\n\n## Description\n\ny\n"), true)
	}); !refused(err) {
		t.Errorf("duplicate section: %v", err)
	}
}

func TestManualStillRefused(t *testing.T) {
	v, rel := noteVault(t, "P-1", "source: manual\n", "")
	c := Ctx{Vault: v, Workspace: "P-1"}
	for _, k := range []string{"title", "type", "source", "source-url", "ignore-x", "covers"} {
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) { o, err := SetField(s, k, "x"); return o, Target{Field: k}, err }); !refused(err) {
			t.Errorf("field %s: %v", k, err)
		}
	}
	// a heading titled like a content section in a work section
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return Append(s, "Tasks", []byte("## Description\nx\n"), 0) }); !refused(err) {
		t.Errorf("work-section heading: %v", err)
	}
}

func TestManualCoveredByEpic(t *testing.T) {
	v, rel := noteVault(t, "P-2", "source: manual\n", "")
	os.MkdirAll(filepath.Join(v, "tickets", "E-1"), 0o755)
	os.WriteFile(filepath.Join(v, "tickets", "E-1", "E-1.md"), []byte("---\ntitle: E\nticket-type: epic\ncovers: [\"[[P-2]]\"]\n---\n# E\n"), 0o644)
	c := Ctx{Vault: v, Workspace: "E-1"}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, "Description", []byte("x\n"), false) }); !refused(err) {
		t.Errorf("covered content: %v", err)
	}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, "Tasks", []byte("x\n"), false) }); err != nil {
		t.Errorf("covered tasks: %v", err)
	}
}

func TestNotManualRefused(t *testing.T) {
	for _, fm := range []string{"source: jira\n", "", "source:\n", "source: other\n"} {
		v, rel := noteVault(t, "P-1", fm, "")
		c := Ctx{Vault: v, Workspace: "P-1"}
		err := write(c, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, "Description", []byte("x\n"), false) })
		if !refused(err) || !strings.Contains(err.Error(), "source is not manual") {
			t.Errorf("%q: %v", fm, err)
		}
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
			return SetSection(s, "T", []byte("\n## Description\n\nx\n"), true)
		}); !refused(err) {
			t.Errorf("%q H1: %v", fm, err)
		}
	}
	if err := write(Ctx{Vault: t.TempDir(), Workspace: "P-9"}, "tickets/P-9/P-9.md", nil); err == nil {
		t.Error("missing note")
	}
}

func TestSourceBlockProtected(t *testing.T) {
	blk := "## Source (Jira)\n\n<!-- source:start -->\ns\n<!-- source:end -->\n\n"
	for _, fm := range []string{"source: jira\n", "source: manual\n"} {
		v, rel := noteVault(t, "P-1", fm, blk)
		c := Ctx{Vault: v, Workspace: "P-1"}
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, "Source (Jira)", []byte("x\n"), false) }); !refused(err) {
			t.Errorf("%q set block: %v", fm, err)
		}
		b, _ := os.ReadFile(filepath.Join(v, rel))
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
			return SetSection(s, "T", []byte(strings.Replace(string(b[strings.Index(string(b), "# T\n")+4:]), "\ns\n", "\nchanged\n", 1)), true)
		}); !refused(err) {
			t.Errorf("%q H1 block change: %v", fm, err)
		}
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
			return Append(s, "Tasks", []byte("x\n<!-- source:end -->\n"), 0)
		}); !refused(err) {
			t.Errorf("%q marker in content: %v", fm, err)
		}
		if err := write(c, rel, func(s []byte) ([]byte, Target, error) { return SetSection(s, "Tasks", []byte("x\n"), false) }); err != nil {
			t.Errorf("%q tasks: %v", fm, err)
		}
	}
}

func TestManualDuplicateContentSection(t *testing.T) {
	v, rel := noteVault(t, "P-1", "source: manual\n", "")
	c := Ctx{Vault: v, Workspace: "P-1"}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
		return SetSection(s, "T", []byte("\n## Description\n\nx\n"), false)
	}); !refused(err) {
		t.Errorf("set-section of H1 without subsections: %v", err)
	}
	if err := write(c, rel, func(s []byte) ([]byte, Target, error) {
		return Append(s, "T", []byte("\n## Description\n\nx\n"), 0)
	}); !refused(err) {
		t.Errorf("append to H1: %v", err)
	}
}

func TestManualDecisionFailsClosed(t *testing.T) {
	if !isManual([]byte("---\nsource: manual\n---\n# T\n")) {
		t.Error("plain manual")
	}
	for _, n := range []string{
		"---\nsource: manual\nsource: jira\n---\n# T\n",
		"---\nsource: jira\nsource: manual\n---\n# T\n",
		"---\nsource: manual\nbad: [unclosed\n---\n# T\n",
	} {
		if isManual([]byte(n)) {
			t.Errorf("manual: %q", n)
		}
	}
}

func TestH1GuardRefusals(t *testing.T) {
	v, rel := noteVault(t, "P-1", "source: manual\n", "## Source (Jira)\n\n<!-- source:start -->\ns\n<!-- source:end -->\n\n")
	c := Ctx{Vault: v, Workspace: "P-1"}
	src, _ := os.ReadFile(filepath.Join(v, rel))
	h1 := Target{Section: []string{"T"}, Level: 1}
	for name, out := range map[string]string{
		"title":  strings.Replace(string(src), "title: T", "title: U", 1),
		"source": strings.Replace(string(src), "source: manual", "source: jira", 1),
		"block":  strings.Replace(string(src), "<!-- source:start -->\ns\n<!-- source:end -->\n", "", 1),
	} {
		if err := checkWrite(c, rel, src, []byte(out), h1); !refused(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCheckMissingNote(t *testing.T) {
	err := Check(Ctx{Vault: t.TempDir(), Workspace: "P-9"}, "tickets/P-9/P-9.md", Target{Section: []string{"Description"}})
	if !refused(err) || !strings.Contains(err.Error(), "source: none") {
		t.Errorf("%v", err)
	}
}
