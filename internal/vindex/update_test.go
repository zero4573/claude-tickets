package vindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, vault, rel, body string) {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T, embedder string) string {
	t.Helper()
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", embedder)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	v := t.TempDir()
	put(t, v, "a.md", "---\ntype: project\ntags: [x, y]\nprojects: [acme]\ndepends-on: [[b]]\ngroups: [\"[[g1]]\"]\n---\n# A\n\nIntro with [[b|the bee]] and ![[c]].\n\n## Part\n\nMore text about [[d]].\n")
	put(t, v, "sub/b.md", "# B\n\nAlpha beta.\n")
	put(t, v, "raw/skip.md", "# R\n\nraw\n")
	put(t, v, "archive/skip.md", "# R\n\narchived\n")
	put(t, v, "templates/skip.md", "# R\n\ntemplate\n")
	put(t, v, ".obsidian/skip.md", "# R\n\nhidden\n")
	put(t, v, "notes.txt", "not markdown")
	return v
}

func TestUpdateFlow(t *testing.T) {
	v := setup(t, "stub")
	st, err := Update(v, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Notes != 2 || st.Changed != 2 || st.Chunks != 3 || st.Embedded != 3 || st.KeywordOnly || st.Model != "stub" {
		t.Fatalf("first: %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(v, ".ct", ".gitignore")); string(b) != "*\n" {
		t.Fatalf("gitignore %q", b)
	}
	ix, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	a := ix.Notes[0]
	if a.Path != "a.md" || a.Name != "a" || a.Type != "project" || len(a.Tags) != 2 || a.Projects[0] != "acme" {
		t.Fatalf("note %+v", a)
	}
	if got := strings.Join(a.Links, ","); got != "b,g1,c,d" {
		t.Fatalf("links %s", got)
	}
	if ix.Notes[1].Path != "sub/b.md" || ix.Meta.Dims != 64 || len(ix.Vector(0)) != 64 || ix.Chunks[0].Note != 0 {
		t.Fatalf("index %+v", ix.Meta)
	}
	if strings.Contains(ix.Chunks[0].Text, "[[") || !strings.Contains(ix.Chunks[0].Text, "the bee and c") {
		t.Fatalf("text not cleaned: %q", ix.Chunks[0].Text)
	}
	if fi, _ := os.Stat(filepath.Join(Dir(v), "vectors.f32")); fi.Size() != 3*64*4 {
		t.Fatalf("vectors size %d", fi.Size())
	}

	// Second run: nothing.
	st, _ = Update(v, UpdateOptions{})
	if st.Embedded != 0 || st.Changed != 0 || st.Removed != 0 || st.Notes != 2 {
		t.Fatalf("second: %+v", st)
	}

	// Edit one section of a: only its changed chunk is embedded.
	put(t, v, "a.md", strings.Replace(read(t, v, "a.md"), "More text about", "Other words about", 1))
	st, _ = Update(v, UpdateOptions{})
	if st.Changed != 1 || st.Embedded != 1 || st.Chunks != 3 {
		t.Fatalf("edit: %+v", st)
	}

	// Touch without change: hash path, nothing embedded, mtime recorded.
	future := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(v, "sub/b.md"), future, future)
	st, _ = Update(v, UpdateOptions{})
	if st.Changed != 0 || st.Embedded != 0 {
		t.Fatalf("touch: %+v", st)
	}
	ix, _ = Load(v)
	if ix.Notes[1].MTime != future.UnixNano() {
		t.Fatal("mtime not updated")
	}

	// Delete a note.
	os.Remove(filepath.Join(v, "sub/b.md"))
	st, _ = Update(v, UpdateOptions{})
	if st.Removed != 1 || st.Notes != 1 || st.Chunks != 2 {
		t.Fatalf("delete: %+v", st)
	}

	// --full re-embeds everything.
	st, _ = Update(v, UpdateOptions{Full: true})
	if st.Embedded != 2 {
		t.Fatalf("full: %+v", st)
	}

	// Chunk size change forces a rebuild.
	st, _ = Update(v, UpdateOptions{ChunkTokens: 64})
	if st.Embedded != 2 {
		t.Fatalf("chunk tokens: %+v", st)
	}
	ix, _ = Load(v)
	if ix.Meta.ChunkTokens != 64 {
		t.Fatal("chunk_tokens not kept")
	}
	st, _ = Update(v, UpdateOptions{})
	if st.Embedded != 0 {
		t.Fatalf("kept chunk size: %+v", st)
	}
}

func read(t *testing.T, v, rel string) string {
	b, err := os.ReadFile(filepath.Join(v, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeywordOnlyThenModel(t *testing.T) {
	v := setup(t, "none")
	st, err := Update(v, UpdateOptions{})
	if err != nil || !st.KeywordOnly || st.Embedded != 0 || st.Chunks != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	ix, _ := Load(v)
	if ix.Meta.Model != "" || ix.Meta.Dims != 0 || ix.Vector(0) != nil {
		t.Fatalf("meta %+v", ix.Meta)
	}
	if _, err := os.Stat(filepath.Join(Dir(v), "vectors.f32")); err == nil {
		t.Fatal("vectors.f32 should be absent")
	}
	if _, err := os.Stat(filepath.Join(Dir(v), "model-hint")); err != nil {
		t.Fatal("hint marker missing")
	}
	// A model appearing rebuilds with vectors.
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	st, _ = Update(v, UpdateOptions{})
	if st.Embedded != 3 || st.KeywordOnly {
		t.Fatalf("%+v", st)
	}
	// And a model change (stub -> none) goes back to keyword-only.
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	st, _ = Update(v, UpdateOptions{})
	if !st.KeywordOnly || st.Chunks != 3 {
		t.Fatalf("%+v", st)
	}
}

func TestCorruptIndexRebuilt(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	os.WriteFile(filepath.Join(Dir(v), "meta.json"), []byte("{not json"), 0o644)
	if _, err := Load(v); err == nil {
		t.Fatal("corrupt meta should not load")
	}
	st, err := Update(v, UpdateOptions{})
	if err != nil || st.Embedded != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	// Counts out of step with the files.
	os.WriteFile(filepath.Join(Dir(v), "chunks.jsonl"), nil, 0o644)
	if _, err := Load(v); err == nil {
		t.Fatal("mismatch should not load")
	}
	st, _ = Update(v, UpdateOptions{})
	if st.Embedded != 3 {
		t.Fatalf("%+v", st)
	}
	if _, err := Load(v); err != nil {
		t.Fatal(err)
	}
}

func TestRefresh(t *testing.T) {
	v := setup(t, "stub")
	Refresh(v, "a.md") // no index: no-op
	if _, err := os.Stat(Dir(v)); err == nil {
		t.Fatal("Refresh must not create an index")
	}
	Update(v, UpdateOptions{})
	put(t, v, "sub/b.md", "# B\n\nChanged text.\n")
	put(t, v, "new.md", "# N\n\nnew\n")
	Refresh(v, "sub/b.md")
	ix, _ := Load(v)
	if len(ix.Notes) != 2 || !strings.Contains(ix.Chunks[ix.Notes[1].First].Text, "Changed") {
		t.Fatalf("refresh: %d notes", len(ix.Notes))
	}
	Refresh(v, filepath.Join(v, "new.md")) // absolute path
	ix, _ = Load(v)
	if len(ix.Notes) != 3 {
		t.Fatalf("new note not indexed: %d", len(ix.Notes))
	}
}

func TestCleanLinks(t *testing.T) {
	for in, want := range map[string]string{
		"[[a|b]] x":  "b x",
		"[[a]]":      "a",
		"![[a.png]]": "a.png",
		"[[a#h]]":    "a#h",
		"no links":   "no links",
	} {
		if got := cleanLinks(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
