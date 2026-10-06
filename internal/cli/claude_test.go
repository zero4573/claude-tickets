package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelationsPrompt(t *testing.T) {
	v := filepath.Join(t.TempDir(), "My Vault")
	note := func(name, body string) {
		p := filepath.Join(v, "projects", name, name+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\n"+body+"---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	note("web", "type: project\n")
	note("lib", "type: project\n")
	note("cli", "type: project\ndepends-on: [\"[[lib]]\"]\n")

	var b strings.Builder
	relationsPrompt(&b, v, "web")
	if b.Len() != 0 {
		t.Errorf("standalone repo got %q", b.String())
	}
	relationsPrompt(&b, v, "cli")
	got := b.String()
	for _, want := range []string{
		"- It depends on `lib`.\n",
		"- Run `ct vault groups \"" + v + "\" cli` for the projects it works with.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	b.Reset()
	relationsPrompt(&b, v, "lib")
	if !strings.Contains(b.String(), "- `cli` depends on it.\n") {
		t.Errorf("lib: %q", b.String())
	}
}
