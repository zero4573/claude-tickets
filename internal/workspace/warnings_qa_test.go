package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// QA (MAN-15): Read's "not inside the workspace" warning goes to Warnings
// (os.Stderr by default), so ct status --watch can keep it in its frame.
func TestReadWarnsToWarnings(t *testing.T) {
	if Warnings != os.Stderr {
		t.Fatalf("Warnings defaults to %v, want os.Stderr", Warnings)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "repo-in")
	data := `{"id":"MAN-1","repos":[{"slug":"in","path":"` + filepath.ToSlash(in) + `"},{"slug":"out","path":"/elsewhere/x"},{"slug":"rel","path":"x"}]}`
	if err := os.WriteFile(File(dir), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := Warnings
	Warnings = &buf
	t.Cleanup(func() { Warnings = old })
	info, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Repos) != 1 || info.Repos[0].Slug != "in" {
		t.Errorf("kept %+v, want only in", info.Repos)
	}
	got := buf.String()
	if strings.Count(got, "\n") != 2 || !strings.Contains(got, `ignoring out at "/elsewhere/x"`) || !strings.Contains(got, `ignoring rel at "x"`) {
		t.Errorf("warnings %q", got)
	}
}

// QA (MAN-15): a missing work root has no stamps, and its first workspace
// is a change (the "no workspaces" to table transition).
func TestStateStampsMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work-a")
	empty := StateStamps(root)
	if len(empty) != 0 || empty.Changed(StateStamps(root)) {
		t.Fatalf("missing root: %v", empty)
	}
	ws := filepath.Join(root, "MAN-1")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(ws), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !empty.Changed(StateStamps(root)) {
		t.Error("a first workspace isn't a change")
	}
}
