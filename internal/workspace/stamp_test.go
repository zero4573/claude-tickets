package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func stampRoot(t *testing.T) (root, a, b string) {
	t.Helper()
	root = t.TempDir()
	a, b = filepath.Join(root, "MAN-1"), filepath.Join(root, "MAN-2")
	for _, d := range []string{a, b, filepath.Join(root, ".kb-x")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(File(d), []byte(`{"id": "x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeAgentState(t, a, `{"state": "working"}`)
	return root, a, b
}

func writeAgentState(t *testing.T, dir, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".agent-state"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStateStampsUnchanged(t *testing.T) {
	root, _, _ := stampRoot(t)
	s := StateStamps(root)
	if len(s) != 2 {
		t.Fatalf("want 2 ticket workspaces (no kb), got %v", s)
	}
	if s.Changed(StateStamps(root)) {
		t.Error("nothing changed, but Changed is true")
	}
}

func TestStateStampsChanges(t *testing.T) {
	old := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		change func(t *testing.T, root, a, b string)
	}{
		{"created", func(t *testing.T, root, a, b string) { writeAgentState(t, b, `{}`) }},
		{"deleted", func(t *testing.T, root, a, b string) {
			if err := os.Remove(filepath.Join(a, ".agent-state")); err != nil {
				t.Fatal(err)
			}
		}},
		{"modified in place", func(t *testing.T, root, a, b string) {
			if err := os.Chtimes(filepath.Join(a, ".agent-state"), old, old.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
		}},
		{"resized in place", func(t *testing.T, root, a, b string) {
			f := filepath.Join(a, ".agent-state")
			writeAgentState(t, a, `{"state": "needs-input"}`)
			if err := os.Chtimes(f, old, old); err != nil {
				t.Fatal(err)
			}
		}},
		{"workspace added", func(t *testing.T, root, a, b string) {
			d := filepath.Join(root, "MAN-3")
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(File(d), []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"workspace removed", func(t *testing.T, root, a, b string) {
			if err := os.RemoveAll(b); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, a, b := stampRoot(t)
			// a fixed mtime, so the in-place cases don't depend on the FS's
			// timestamp granularity
			if err := os.Chtimes(filepath.Join(a, ".agent-state"), old, old); err != nil {
				t.Fatal(err)
			}
			before := StateStamps(root)
			c.change(t, root, a, b)
			if !before.Changed(StateStamps(root)) {
				t.Error("change not detected")
			}
		})
	}
}

func TestStateStampsIgnoresKB(t *testing.T) {
	root, _, _ := stampRoot(t)
	before := StateStamps(root)
	writeAgentState(t, filepath.Join(root, ".kb-x"), `{"state": "working"}`)
	if before.Changed(StateStamps(root)) {
		t.Error("a kb workspace's .agent-state counted as a change")
	}
}

// The hooks write .agent-state.tmp and rename it over .agent-state: a new
// file, detected even with the same size and mtime.
func TestStateStampsRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file identity across a rename isn't checked on Windows")
	}
	root, a, _ := stampRoot(t)
	f := filepath.Join(a, ".agent-state")
	old := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	before := StateStamps(root)
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, []byte(`{"state": "idle!!!"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, f); err != nil {
		t.Fatal(err)
	}
	after := StateStamps(root)
	if x, y := before[a], after[a]; x.size != y.size || !x.mod.Equal(y.mod) {
		t.Fatalf("test setup: want same size and mtime, got %v/%v and %v/%v", x.size, x.mod, y.size, y.mod)
	}
	if !before.Changed(after) {
		t.Error("rename over .agent-state not detected")
	}
}
