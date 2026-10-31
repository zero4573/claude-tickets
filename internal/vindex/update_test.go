package vindex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zero4573/tix/internal/embed"
	"github.com/zero4573/tix/internal/modelcache"
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
	t.Setenv("TIX_EMBEDDER", embedder)
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
	if b, _ := os.ReadFile(filepath.Join(v, ".tix", ".gitignore")); string(b) != "*\n" {
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
	t.Setenv("TIX_EMBEDDER", "stub")
	st, _ = Update(v, UpdateOptions{})
	if st.Embedded != 3 || st.KeywordOnly {
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

func zeroRows(ix *Index) (n int) {
	for i := range ix.Chunks {
		if isZero(ix.Vector(i)) {
			n++
		}
	}
	return n
}

// A missing model never throws vectors away: changed notes get zero rows,
// the next Update with the model embeds exactly those.
func TestMissingModelKeepsVectors(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIX_EMBEDDER", "none")
	put(t, v, "sub/b.md", "# B\n\nBrand new words.\n")
	put(t, v, "c.md", "# C\n\nAnother note.\n")
	st, err := Update(v, UpdateOptions{})
	if err != nil || st.Changed != 2 || st.Embedded != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	ix, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Meta.Model != "stub" || ix.Meta.Dims != 64 || len(ix.Chunks) != 4 || zeroRows(ix) != 2 {
		t.Fatalf("meta %+v zero=%d", ix.Meta, zeroRows(ix))
	}
	if isZero(ix.Vector(0)) || isZero(ix.Vector(1)) {
		t.Fatal("untouched note lost its vectors")
	}
	if !strings.Contains(strings.Join(ix.Notes[0].Links, ","), "b") || !strings.Contains(ix.Chunks[ix.Notes[2].First].Text, "Brand new") {
		t.Fatal("text and metadata must update without a model")
	}
	// Idempotent without a model.
	if st, _ = Update(v, UpdateOptions{}); st.Changed != 0 || st.Embedded != 0 {
		t.Fatalf("%+v", st)
	}
	// The model returns: only the zero rows are embedded (notes unchanged on disk).
	t.Setenv("TIX_EMBEDDER", "stub")
	st, err = Update(v, UpdateOptions{})
	if err != nil || st.Embedded != 2 || st.Changed != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	ix, _ = Load(v)
	if zeroRows(ix) != 0 || ix.Meta.Model != "stub" {
		t.Fatalf("zero rows left: %d", zeroRows(ix))
	}
	if st, _ = Update(v, UpdateOptions{}); st.Embedded != 0 {
		t.Fatalf("%+v", st)
	}
	// An explicit --full without a model is the keyword-only rebuild.
	t.Setenv("TIX_EMBEDDER", "none")
	st, _ = Update(v, UpdateOptions{Full: true})
	ix, _ = Load(v)
	if !st.KeywordOnly || ix.Meta.Dims != 0 {
		t.Fatalf("%+v %+v", st, ix.Meta)
	}
}

func TestLoadRequiresContiguousChunks(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	ix, _ := Load(v)
	ix.Notes[1].First++ // a gap before note 1's rows
	if err := writeJSONL(nil, filepath.Join(Dir(v), "notes.jsonl"), ix.Notes); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("got %v", err)
	}
	// Update rebuilds it, reusing the torn index's vectors by chunk hash.
	if st, err := Update(v, UpdateOptions{}); err != nil || st.Embedded != 0 || st.Chunks != 3 {
		t.Fatalf("%+v %v", st, err)
	}
}

// Load takes the commit lock: a reader started in a writer's torn window waits
// for the writer instead of reporting a half-written index.
func TestLoadWaitsForWriter(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	put(t, v, "sub/b.md", "# B\n\nNew text so a chunk is added.\n\n## More\n\nand more\n")
	type res struct {
		ix  *Index
		err error
	}
	got := make(chan res, 1)
	var early bool
	beforeNotesWrite = func() {
		go func() { ix, err := Load(v); got <- res{ix, err} }()
		select {
		case <-got:
			early = true
		case <-time.After(300 * time.Millisecond):
		}
	}
	defer func() { beforeNotesWrite = nil }()
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if early {
		t.Fatal("Load returned while the writer held the lock")
	}
	r := <-got
	if r.err != nil || len(r.ix.Chunks) != 4 {
		t.Fatalf("%v", r.err)
	}
}

func TestFilesSkipsSymlinks(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	if err := os.Symlink(filepath.Join(v, "sub", "b.md"), filepath.Join(v, "link.md")); err != nil {
		t.Skip("no symlinks")
	}
	Refresh(v, "link.md")
	ix, _ := Load(v)
	if len(ix.Notes) != 2 {
		t.Fatalf("symlinked note indexed: %d notes", len(ix.Notes))
	}
}

type blocking struct {
	embed.Embedder
	entered chan struct{}
	release chan struct{}
}

func (b blocking) Embed(texts []string, q bool) ([][]float32, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return b.Embedder.Embed(texts, q)
}

// W3 S03: Load takes only the commit lock, so it returns the old index while
// a writer is inside Embed; a second Update still waits on index.lock.
func TestLoadDoesNotWaitForEmbedding(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, v, "sub/b.md", "# B\n\nChanged text to embed.\n")
	b := blocking{embed.Stub(64), make(chan struct{}, 1), make(chan struct{})}
	openEmbedder = func(string) (embed.Embedder, error) { return b, nil }
	defer func() { openEmbedder = modelcache.Open }()
	done := make(chan error, 1)
	go func() { _, err := Update(v, UpdateOptions{}); done <- err }()
	select {
	case <-b.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Update never reached Embed")
	}
	loaded := make(chan *Index, 1)
	go func() { ix, _ := Load(v); loaded <- ix }()
	select {
	case ix := <-loaded:
		if ix == nil || len(ix.Chunks) != 3 {
			t.Fatalf("old index: %+v", ix)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Load waited for the embedding writer")
	}
	close(b.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// W3 C02: a note under a symlinked directory is not added by a Files refresh.
func TestFilesSkipsSymlinkedDir(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	if err := os.Symlink(filepath.Join(v, "sub"), filepath.Join(v, "link-dir")); err != nil {
		t.Skip("no symlinks")
	}
	put(t, v, "sub/n.md", "# N\n\nnew\n")
	Refresh(v, "link-dir/n.md")
	ix, _ := Load(v)
	for _, n := range ix.Notes {
		if strings.HasPrefix(n.Path, "link-dir/") {
			t.Fatalf("indexed %s", n.Path)
		}
	}
	Refresh(v, "sub/n.md")
	ix, _ = Load(v)
	if len(ix.Notes) != 3 {
		t.Fatalf("real path not indexed: %d", len(ix.Notes))
	}
}

// A crash between the renames (chunks and vectors renamed, notes and meta not)
// leaves a set Load must refuse (ErrNoIndex), and the next Update repairs it.
func TestTornIndexDetected(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	put(t, v, "sub/b.md", "# B\n\nNew text so a chunk is added.\n\n## More\n\nand more\n")
	beforeNotesWrite = func() { panic("crash") }
	defer func() { beforeNotesWrite = nil }()
	func() {
		defer func() { recover() }()
		Update(v, UpdateOptions{})
	}()
	beforeNotesWrite = nil
	if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("torn index loaded: %v", err)
	}
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(v)
	if err != nil || len(ix.Chunks) != 4 {
		t.Fatalf("not repaired: %v", err)
	}
}

// An index written before meta.json carried file hashes is rebuilt once.
func TestOldMetaWithoutFilesRebuilds(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	p := filepath.Join(Dir(v), "meta.json")
	b, _ := os.ReadFile(p)
	if i := strings.Index(string(b), `,"files"`); i > 0 {
		os.WriteFile(p, append([]byte(string(b[:i])), '}', '\n'), 0o644)
	}
	if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("old meta loaded: %v", err)
	}
	if st, err := Update(v, UpdateOptions{}); err != nil || st.Changed != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := Load(v); err != nil {
		t.Fatal(err)
	}
}

// W4 C01: a vault reached through a symlink still takes a Files refresh.
func TestFilesVaultBehindSymlink(t *testing.T) {
	v := setup(t, "stub")
	Update(v, UpdateOptions{})
	link := filepath.Join(t.TempDir(), "vault-link")
	if err := os.Symlink(v, link); err != nil {
		t.Skip("no symlinks")
	}
	put(t, v, "sub/n.md", "# N\n\nnew\n")
	Refresh(link, "sub/n.md")
	ix, err := Load(v)
	if err != nil || len(ix.Notes) != 3 {
		t.Fatalf("not indexed through the symlinked vault: %v", err)
	}
}

// W5 S02: replacing any one index file with another valid generation is a torn
// index; a touch-only update (same content) keeps the index loadable.
func TestMixedGenerationsRefused(t *testing.T) {
	for _, name := range []string{"notes.jsonl", "chunks.jsonl", "vectors.f32"} {
		t.Run(name, func(t *testing.T) {
			v := setup(t, "stub")
			if _, err := Update(v, UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			old, err := os.ReadFile(filepath.Join(Dir(v), name))
			if err != nil {
				t.Fatal(err)
			}
			put(t, v, "sub/b.md", "# B\n\nChanged text for a new generation.\n\n## More\n\nand more\n")
			put(t, v, "c.md", "# C\n\nBrand new note.\n")
			if _, err := Update(v, UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(v); err != nil {
				t.Fatal(err)
			}
			cur, _ := os.ReadFile(filepath.Join(Dir(v), name))
			if string(cur) == string(old) {
				t.Fatalf("%s did not change between generations", name)
			}
			if err := os.WriteFile(filepath.Join(Dir(v), name), old, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
				t.Fatalf("mixed generation loaded: %v", err)
			}
		})
	}
}

func TestTouchOnlyUpdateStaysLoadable(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(v, "a.md"), later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(v); err != nil {
		t.Fatalf("touch-only update broke the index: %v", err)
	}
}

// A torn index (notes.jsonl no longer matches meta.json) is rebuilt, but its
// vectors still serve as a cache by chunk hash: nothing unchanged is embedded
// again, and the result equals a fresh build's.
func TestTornIndexReusesVectors(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, v, "sub/b.md", "# B\n\nAlpha beta changed.\n")
	if err := os.WriteFile(filepath.Join(Dir(v), "notes.jsonl"), []byte("torn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("torn index loaded: %v", err)
	}
	st, err := Update(v, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Chunks != 3 || st.Embedded >= st.Chunks || st.Embedded == 0 {
		t.Fatalf("want only the changed chunk embedded: %+v", st)
	}
	got, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Update(v, UpdateOptions{Full: true}); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != len(fresh.Chunks) || len(got.vecs) != len(fresh.vecs) {
		t.Fatal("shape differs from a fresh build")
	}
	for i := range got.vecs {
		if got.vecs[i] != fresh.vecs[i] {
			t.Fatalf("vector %d differs from a fresh build", i)
		}
	}
}

// negStub is a stub embedder under another id whose vectors are negated.
type negStub struct{ embed.Embedder }

func (negStub) ID() string { return "stub2" }
func (n negStub) Embed(texts []string, q bool) ([][]float32, error) {
	vs, err := n.Embedder.Embed(texts, q)
	for _, v := range vs {
		for i := range v {
			v[i] = -v[i]
		}
	}
	return vs, err
}

// A commit torn after vectors.f32 (of a new model, same dims) was renamed
// leaves the old meta next to foreign vectors: they must not be reused (W7 S01).
func TestSalvageRefusesForeignVectors(t *testing.T) {
	v := setup(t, "stub")
	if _, err := Update(v, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	orig := openEmbedder
	defer func() { openEmbedder = orig; beforeNotesWrite = nil }()
	openEmbedder = func(string) (embed.Embedder, error) { return negStub{embed.Stub(64)}, nil }
	beforeNotesWrite = func() { panic("crash") }
	func() {
		defer func() { recover() }()
		Update(v, UpdateOptions{Model: "stub2"})
	}()
	beforeNotesWrite = nil
	openEmbedder = orig
	if _, err := Load(v); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("torn index loaded: %v", err)
	}
	st, err := Update(v, UpdateOptions{Model: "stub"})
	if err != nil || st.Embedded != st.Chunks || st.Chunks == 0 {
		t.Fatalf("foreign vectors reused: %+v %v", st, err)
	}
}

// The salvage is refused when the torn index's model or sha is not the one
// being built.
func TestSalvageRefusesModelAndShaMismatch(t *testing.T) {
	tear := func(t *testing.T) string {
		v := setup(t, "stub")
		if _, err := Update(v, UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(Dir(v), "notes.jsonl"), []byte("torn\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("model", func(t *testing.T) {
		v := tear(t)
		orig := openEmbedder
		defer func() { openEmbedder = orig }()
		openEmbedder = func(string) (embed.Embedder, error) { return negStub{embed.Stub(64)}, nil }
		st, err := Update(v, UpdateOptions{Model: "stub2"})
		if err != nil || st.Embedded != st.Chunks {
			t.Fatalf("%+v %v", st, err)
		}
	})
	t.Run("sha", func(t *testing.T) {
		v := tear(t)
		p := filepath.Join(Dir(v), "meta.json")
		b, _ := os.ReadFile(p)
		b = []byte(strings.Replace(string(b), `"model_sha256":""`, `"model_sha256":"abc"`, 1))
		if !strings.Contains(string(b), `"abc"`) {
			t.Fatalf("meta: %s", b)
		}
		os.WriteFile(p, b, 0o644)
		st, err := Update(v, UpdateOptions{})
		if err != nil || st.Embedded != st.Chunks {
			t.Fatalf("%+v %v", st, err)
		}
	})
}

// A vault's .ct folder (the index under tix's old name) becomes .tix, once
// and only when .tix doesn't exist
func TestMigrateLegacy(t *testing.T) {
	v := t.TempDir()
	put(t, v, ".ct/index/meta.json", "{}")
	MigrateLegacy(v)
	if _, err := os.Stat(filepath.Join(Dir(v), "meta.json")); err != nil {
		t.Fatalf("not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(v, ".ct")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".ct left: %v", err)
	}
	put(t, v, ".ct/index/meta.json", "old")
	MigrateLegacy(v)
	if data, _ := os.ReadFile(filepath.Join(Dir(v), "meta.json")); string(data) != "{}" {
		t.Errorf("an existing .tix was replaced: %q", data)
	}
}
