package vindex

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// memSource is a fake source: documents in a map, no modification times.
type memSource struct{ docs map[string]string }

func (m *memSource) List() ([]string, error) {
	var out []string
	for p := range m.docs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (m *memSource) Normalize(p string) (string, bool) {
	_, ok := m.docs[p]
	return p, ok
}

func (m *memSource) Stat(rel string) (FileInfo, error) {
	d, ok := m.docs[rel]
	if !ok {
		return FileInfo{}, fs.ErrNotExist
	}
	return FileInfo{Size: int64(len(d))}, nil
}

func (m *memSource) Read(rel string) ([]byte, error) { return []byte(m.docs[rel]), nil }

func TestSecondSourceIndexedAndRemoved(t *testing.T) {
	t.Setenv("TIX_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	src := &memSource{docs: map[string]string{
		"specs/a/spec.md": "# A\n\nAlpha.\n",
		"specs/b/spec.md": "# B\n\nBeta.\n",
	}}
	st, err := UpdateSource(root, src, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Notes != 2 || st.Changed != 2 || st.Embedded != 2 {
		t.Fatalf("first: %+v", st)
	}
	ix, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Notes[0].Path != "specs/a/spec.md" || ix.Notes[1].Path != "specs/b/spec.md" || ix.Notes[0].Name != "spec" {
		t.Fatalf("notes %+v", ix.Notes)
	}

	st, _ = UpdateSource(root, src, UpdateOptions{})
	if st.Changed != 0 || st.Removed != 0 || st.Embedded != 0 {
		t.Fatalf("unchanged: %+v", st)
	}

	// Changed content (same mtime 0, new size) is re-read; a removed document goes.
	src.docs["specs/a/spec.md"] = "# A\n\nAlpha and more.\n"
	delete(src.docs, "specs/b/spec.md")
	st, err = UpdateSource(root, src, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Changed != 1 || st.Removed != 1 || st.Notes != 1 {
		t.Fatalf("edit+remove: %+v", st)
	}
	ix, _ = Load(root)
	if len(ix.Notes) != 1 || !strings.Contains(ix.Chunks[0].Text, "and more") {
		t.Fatalf("after: %+v %+v", ix.Notes, ix.Chunks)
	}

	// Removing every document leaves an empty, loadable index.
	src.docs = map[string]string{}
	if st, err = UpdateSource(root, src, UpdateOptions{}); err != nil || st.Removed != 1 || st.Notes != 0 {
		t.Fatalf("empty: %+v %v", st, err)
	}
	if ix, err = Load(root); err != nil || len(ix.Notes) != 0 {
		t.Fatalf("empty load: %v", err)
	}

	// A Files refresh goes through Normalize: unknown paths are ignored.
	src.docs["specs/c/spec.md"] = "# C\n"
	if st, err = UpdateSource(root, src, UpdateOptions{Files: []string{"specs/c/spec.md", "nope.md"}}); err != nil || st.Notes != 1 {
		t.Fatalf("files: %+v %v", st, err)
	}
}

// The vault source is the same code path as Update: an index Update built
// is current for UpdateSource(VaultSource) and loads unchanged.
func TestVaultSourceMatchesUpdate(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	before, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	st, err := UpdateSource(v, VaultSource(v), UpdateOptions{})
	if err != nil || st.Changed != 0 || st.Removed != 0 || st.Embedded != 0 || st.Notes != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	after, _ := Load(v)
	if before.Meta.Updated != after.Meta.Updated || filepath.Base(Dir(v)) != "index" {
		t.Fatalf("index rewritten")
	}
}

func TestIndexableSkipsSpecSnapshots(t *testing.T) {
	for p, want := range map[string]bool{
		"tickets/manual/MAN-1/specs-base/x/a.md":        false,
		"tickets/manual/MAN-1/specs-after/x/a.md":       false,
		"tickets/manual/MAN-1/specs-stage/x/moves/a.md": false,
		"tickets/manual/MAN-1/specs/x/a.md":             true,
		"tickets/manual/MAN-1/MAN-1.md":                 true,
		"projects/x/specs-base/a.md":                    true,
	} {
		if got := indexable(p); got != want {
			t.Errorf("indexable(%q) = %v, want %v", p, got, want)
		}
	}
}
