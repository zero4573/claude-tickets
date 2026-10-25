package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zero4573/claude-tickets/internal/links"
)

func linksMoveVault(t *testing.T) string {
	v := t.TempDir()
	for rel, body := range map[string]string{
		".obsidian/app.json": "{}",
		"inbox/a.md":         "# a\n",
		"notes/b.md":         "see [[inbox/a]]\n",
		"notes/c.md":         "nothing\n",
		"notes/d.md":         "",
		"refs/d.md":          "",
	} {
		p := filepath.Join(v, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func TestLinksMoveRefreshesExactPaths(t *testing.T) {
	v := linksMoveVault(t)
	old := refreshIndex
	var calls [][]string
	refreshIndex = func(_ string, paths ...string) { calls = append(calls, paths) }
	defer func() { refreshIndex = old }()
	cmd := vaultLinksCmd()
	cmd.SetArgs([]string{"move", v, "inbox/a.md", "notes/a2.md"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"inbox/a.md", "notes/a2.md", "notes/b.md"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("refresh calls = %v, want %v", calls, want)
	}
}

func TestLinksMoveRefusedDoesNotRefresh(t *testing.T) {
	v := linksMoveVault(t)
	old := refreshIndex
	var calls [][]string
	refreshIndex = func(_ string, paths ...string) { calls = append(calls, paths) }
	defer func() { refreshIndex = old }()
	for _, dst := range []string{"notes/c.md", "refs/d.md"} { // destination exists; name already used
		cmd := vaultLinksCmd()
		cmd.SetArgs([]string{"move", v, "notes/d.md", dst})
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		if err := cmd.Execute(); err == nil {
			t.Fatalf("move to %s should be refused", dst)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("refused moves refreshed the index: %v", calls)
	}
}

func TestLinksMoveWriteErrorStillRefreshes(t *testing.T) {
	v := linksMoveVault(t)
	for rel, body := range map[string]string{"a/ok.md": "[[inbox/a]]\n", "z/ro/stuck.md": "[[inbox/a]]\n"} {
		p := filepath.Join(v, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bad := filepath.Join(v, "z/ro/stuck.md")
	restore := links.WrapWriteNoteForTest(func(next func(string, []byte) error) func(string, []byte) error {
		return func(f string, data []byte) error {
			if f == bad {
				return errors.New("injected write error")
			}
			return next(f, data)
		}
	})
	defer restore()
	old := refreshIndex
	var calls [][]string
	refreshIndex = func(_ string, paths ...string) { calls = append(calls, paths) }
	defer func() { refreshIndex = old }()
	cmd := vaultLinksCmd()
	cmd.SetArgs([]string{"move", v, "inbox/a.md", "notes/a2.md"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	var ce codeError
	if !errors.As(err, &ce) || ce.code != 2 {
		t.Fatalf("err = %v, want exit 2", err)
	}
	want := [][]string{{"inbox/a.md", "notes/a2.md", "a/ok.md", "notes/b.md"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("refresh calls = %v, want %v", calls, want)
	}
}
