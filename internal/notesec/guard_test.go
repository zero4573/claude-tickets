package notesec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ticket = "---\ntitle: T\nstatus: open\n---\n# T\n\n## Description\n\nd\n\n## Tasks\n\nt\n"

func ticketVault(t *testing.T) (Ctx, string) {
	t.Helper()
	v := t.TempDir()
	dir := filepath.Join(v, "tickets", "P-1")
	if err := os.MkdirAll(filepath.Join(v, ".obsidian"), 0o755); err != nil || os.MkdirAll(dir, 0o755) != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "P-1.md")
	if err := os.WriteFile(f, []byte(ticket), 0o644); err != nil {
		t.Fatal(err)
	}
	return Ctx{Vault: v, Workspace: "P-1"}, f
}

func refused(err error) bool {
	var r *RefusedError
	return errors.As(err, &r)
}

func TestWholeH1Refused(t *testing.T) {
	c, f := ticketVault(t)
	_, err := Apply(c, "tickets/P-1/P-1.md", "o", func(src []byte) ([]byte, Target, error) {
		return SetSection(src, "T", []byte("gone\n"), true)
	})
	if !refused(err) {
		t.Fatalf("want refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "H1") || !strings.Contains(err.Error(), "Description") {
		t.Fatalf("message does not name the protected sections: %v", err)
	}
	if b, _ := os.ReadFile(f); string(b) != ticket {
		t.Fatalf("file changed: %q", b)
	}
}

func TestNoTargetRefused(t *testing.T) {
	c, _ := ticketVault(t)
	_, err := Apply(c, "tickets/P-1/P-1.md", "o", func(src []byte) ([]byte, Target, error) {
		return append(src, 'x'), Target{}, nil
	})
	if !refused(err) {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestBadKeys(t *testing.T) {
	for _, k := range []string{"status: x\ntitle", "a:b", " title", "title ", "", "a b"} {
		if _, err := SetField([]byte(ticket), k, "v"); err == nil {
			t.Errorf("SetField %q accepted", k)
		}
		if _, err := AddItem([]byte(ticket), k, "v"); err == nil {
			t.Errorf("AddItem %q accepted", k)
		}
		if _, err := RemoveItem([]byte(ticket), k, "v"); err == nil {
			t.Errorf("RemoveItem %q accepted", k)
		}
	}
	if _, err := SetField([]byte(ticket), "a.b-c_1", "v"); err != nil {
		t.Error(err)
	}
}

func TestContentHeadings(t *testing.T) {
	c, f := ticketVault(t)
	for _, content := range []string{"## Description\nx\n", "# X\n", "## Other\n", "text\n```\n## Description\n```\n### Sub\n"} {
		_, err := Apply(c, "tickets/P-1/P-1.md", "o", func(src []byte) ([]byte, Target, error) {
			return Append(src, "Tasks", []byte(content), 0)
		})
		want := !strings.Contains(content, "```")
		if refused(err) != want || (!want && err != nil) {
			t.Errorf("content %q: err %v, want refused=%v", content, err, want)
		}
	}
	if b, _ := os.ReadFile(f); !strings.Contains(string(b), "### Sub") || strings.Contains(string(b), "\n## Other") || strings.Contains(string(b), "# X") {
		t.Fatalf("%q", b)
	}
}

func TestSymlinks(t *testing.T) {
	c, _ := ticketVault(t)
	if err := os.MkdirAll(filepath.Join(c.Vault, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Vault, "projects", "a.md"), []byte("# A\n\n## Log\n\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(c.Vault, "projects"), filepath.Join(c.Vault, "tickets", "P-1", "link")); err != nil {
		t.Skip(err)
	}
	_, err := Apply(c, "tickets/P-1/link/a.md", "o", func(src []byte) ([]byte, Target, error) {
		return Append(src, "Log", []byte("y\n"), 0)
	})
	if !refused(err) {
		t.Fatalf("want refusal, got %v", err)
	}
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "o.md"), []byte("# O\n\n## Log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "o.md"), filepath.Join(c.Vault, "tickets", "P-1", "o.md")); err != nil {
		t.Fatal(err)
	}
	_, err = Apply(c, "tickets/P-1/o.md", "o", func(src []byte) ([]byte, Target, error) {
		return Append(src, "Log", []byte("y\n"), 0)
	})
	if !refused(err) {
		t.Fatalf("outside the vault: want refusal, got %v", err)
	}
}

// W3 C01: RealRel gives the path a symlinked note resolves to.
func TestRealRel(t *testing.T) {
	c, _ := ticketVault(t)
	if err := os.WriteFile(filepath.Join(c.Vault, "real.md"), []byte("# R\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(c.Vault, "real.md"), filepath.Join(c.Vault, "tickets", "P-1", "l.md")); err != nil {
		t.Skip(err)
	}
	if r, err := RealRel(c.Vault, "tickets/P-1/l.md"); err != nil || r != "real.md" {
		t.Fatalf("%q %v", r, err)
	}
}
