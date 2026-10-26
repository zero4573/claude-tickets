package modelcache

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/lock"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func tgz(t *testing.T, dir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: dir + "/" + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestPins(t *testing.T) {
	p := pins()
	m := Default()
	if m.ID != "bge-small-en-v1.5" || m.Dims != 384 || m.SHA256 == "" || !strings.HasPrefix(m.QueryPrefix, "Represent this sentence") {
		t.Fatalf("default model: %+v", m)
	}
	if got, ok := Lookup(m.ID); !ok || got != m {
		t.Fatal("Lookup")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup nope")
	}
	for k, a := range p.Llama.Assets {
		if len(a.SHA256) != 64 || a.URL == "" || len(a.Files) == 0 {
			t.Errorf("asset %s incomplete", k)
		}
		hasLlama := false
		for _, f := range a.Files {
			if strings.HasPrefix(f.As, "libllama.") {
				hasLlama = true
			}
		}
		if !hasLlama {
			t.Errorf("asset %s has no libllama", k)
		}
	}
	if _, ok := p.Llama.Assets["linux-amd64"]; !ok {
		t.Error("no pin for linux-amd64")
	}
	// Dropped: no verified prebuilt asset for these.
	for _, k := range []string{"linux-arm64", "darwin-arm64", "darwin-amd64"} {
		if _, ok := p.Llama.Assets[k]; ok {
			t.Errorf("%s should not be pinned", k)
		}
	}
}

func TestStatusAndPaths(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", os.Getenv("CLAUDE_TICKETS_CACHE"))
	m := Default()
	if !strings.HasSuffix(filepath.ToSlash(ModelPath(m)), "/models/bge-small-en-v1.5/"+m.File) {
		t.Fatal(ModelPath(m))
	}
	st := StatusOf(m.ID)
	if st.ModelOK || st.LibOK || st.Problem == "" {
		t.Fatalf("empty cache: %+v", st)
	}
	if st := StatusOf("nope"); st.Problem == "" || !strings.Contains(st.Problem, "unknown model") {
		t.Fatalf("unknown: %+v", st)
	}
	if runtime.GOOS == "windows" {
		if _, err := LibDir(); err == nil {
			t.Fatal("windows has no pinned build")
		}
	}
}

func TestOpenSeam(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	e, err := Open("bge-small-en-v1.5")
	if err != nil || e.ID() != "stub" || e.Dims() != 64 {
		t.Fatalf("stub: %v %v", e, err)
	}
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	if _, err := Open("bge-small-en-v1.5"); !errors.Is(err, embed.ErrUnavailable) {
		t.Fatalf("none: %v", err)
	}
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "")
	if _, err := Open("bge-small-en-v1.5"); !errors.Is(err, embed.ErrUnavailable) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := Open("nope"); err == nil || errors.Is(err, embed.ErrUnavailable) {
		t.Fatalf("unknown id: %v", err)
	}
}

func testPins(t *testing.T, srv *httptest.Server, arc []byte, model []byte) *pinFile {
	p := &pinFile{Models: map[string]modelPin{"m": {File: "m.gguf", URL: srv.URL + "/m", SHA256: sum(model), Dims: 4, Default: true}}}
	p.Llama.Version = "b1"
	p.Llama.Assets = map[string]asset{platformKey(): {
		URL: srv.URL + "/lib", SHA256: sum(arc), Dir: "llama-b1",
		Files: []libFile{{"libllama.so.0.1", "libllama.so.0"}, {"libx.so", "libx.so"}, {"libx.so", "libx-copy.so"}},
	}}
	return p
}

func TestFetch(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", os.Getenv("CLAUDE_TICKETS_CACHE"))
	model := []byte("GGUF fake model")
	arc := tgz(t, "llama-b1", map[string]string{"libllama.so.0.1": "L", "libx.so": "X", "llama-cli": "unlisted"})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/m":
			w.Write(model)
		case "/lib":
			w.Write(arc)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := testPins(t, srv, arc, model)

	var out bytes.Buffer
	if err := fetch(context.Background(), p, srv.Client(), "m", &out); err != nil {
		t.Fatal(err)
	}
	st := statusOf(p, "m")
	if !st.ModelOK || !st.LibOK || st.Problem != "" {
		t.Fatalf("after fetch: %+v", st)
	}
	for _, f := range []string{"libllama.so.0", "libx.so", "libx-copy.so"} {
		if !fileOK(filepath.Join(st.LibDir, f)) {
			t.Errorf("%s missing", f)
		}
	}
	if _, err := os.Stat(filepath.Join(st.LibDir, "llama-cli")); err == nil {
		t.Error("an unlisted file was extracted")
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("downloads: %d", n)
	}
	// Again: nothing downloaded.
	out.Reset()
	if err := fetch(context.Background(), p, srv.Client(), "m", &out); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || strings.Count(out.String(), "present") != 2 {
		t.Fatalf("second fetch hit the network (%d): %s", hits.Load(), out.String())
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(st.LibDir); err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("lib dir mode: %v %v", fi, err)
		}
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(st.LibDir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestFetchBadHash(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", os.Getenv("CLAUDE_TICKETS_CACHE"))
	model := []byte("model")
	arc := tgz(t, "llama-b1", map[string]string{"libllama.so.0.1": "L", "libx.so": "X"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/m" {
			w.Write([]byte("tampered"))
			return
		}
		w.Write(arc)
	}))
	defer srv.Close()
	p := testPins(t, srv, arc, model)
	err := fetch(context.Background(), p, srv.Client(), "m", nil)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want a sha mismatch, got %v", err)
	}
	st := statusOf(p, "m")
	if st.ModelOK {
		t.Fatal("a model failing verification was installed")
	}
	if err := fetch(context.Background(), p, srv.Client(), "nope", nil); err == nil {
		t.Fatal("unknown id")
	}
}

func TestFetchRemovesStale(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", os.Getenv("CLAUDE_TICKETS_CACHE"))
	model := []byte("model")
	arc := tgz(t, "llama-b1", map[string]string{"libllama.so.0.1": "L", "libx.so": "X"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/m" {
			w.Write(model)
			return
		}
		w.Write(arc)
	}))
	defer srv.Close()
	p := testPins(t, srv, arc, model)
	m, _ := modelOf(p, "m")
	libDir, _ := libDirFor(p)
	mdir := filepath.Dir(ModelPath(m))
	os.MkdirAll(mdir, 0o755)
	os.MkdirAll(filepath.Join(filepath.Dir(libDir), ".stage-old"), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(libDir), ".download-x"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(mdir, ".download-y"), []byte("y"), 0o644)
	if err := fetch(context.Background(), p, srv.Client(), "m", nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{mdir, filepath.Dir(libDir)} {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				t.Errorf("leftover %s in %s", e.Name(), d)
			}
		}
	}
}

func TestFetchIdleTimeout(t *testing.T) {
	old := idleTimeout
	idleTimeout = 100 * time.Millisecond
	defer func() { idleTimeout = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("part"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	f, _ := os.CreateTemp(t.TempDir(), "d")
	defer f.Close()
	if err := download(context.Background(), srv.Client(), srv.URL, "00", f); err == nil {
		t.Fatal("a stalled body was not cut")
	}
}

// Two fetches at once (two terminals) both succeed: the second waits for the
// lock instead of deleting the first one's .download-*/.stage-* files.
func TestFetchConcurrent(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", os.Getenv("CLAUDE_TICKETS_CACHE"))
	model := bytes.Repeat([]byte("model"), 1000)
	arc := tgz(t, "llama-b1", map[string]string{"libllama.so.0.1": "L", "libx.so": "X"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		if r.URL.Path == "/m" {
			w.Write(model)
			return
		}
		w.Write(arc)
	}))
	defer srv.Close()
	p := testPins(t, srv, arc, model)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- fetch(context.Background(), p, srv.Client(), "m", nil) }()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if st := statusOf(p, "m"); !st.ModelOK || !st.LibOK {
		t.Fatalf("not installed: %+v", st)
	}
	m, _ := modelOf(p, "m")
	if b, _ := os.ReadFile(ModelPath(m)); sum(b) != sum(model) {
		t.Fatal("model file differs")
	}
}

// Sessions mount the cache read-only: what they call (Status, path
// resolution) must succeed and create nothing there.
func TestReadOnlyCache(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CLAUDE_TICKETS_CACHE", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	m := Default()
	lib, err := LibDir()
	if err != nil {
		t.Skip(err)
	}
	os.MkdirAll(filepath.Dir(ModelPath(m)), 0o755)
	os.WriteFile(ModelPath(m), []byte("gguf"), 0o644)
	os.MkdirAll(lib, 0o755)
	for _, f := range pins().Llama.Assets[platformKey()].Files {
		os.WriteFile(filepath.Join(lib, f.As), []byte("lib"), 0o755)
	}
	list := func() (out []string) {
		filepath.Walk(cache, func(p string, _ os.FileInfo, _ error) error { out = append(out, p); return nil })
		return
	}
	before := list()
	mkdirAll = func(string, os.FileMode) error {
		t.Error("read path called mkdirAll")
		return errors.New("read-only cache")
	}
	createTemp = func(string, string) (*os.File, error) {
		t.Error("read path called createTemp")
		return nil, errors.New("read-only cache")
	}
	t.Cleanup(func() { mkdirAll, createTemp = os.MkdirAll, os.CreateTemp })
	st := StatusOf(m.ID)
	if !st.ModelOK || !st.LibOK || st.Problem != "" {
		t.Fatalf("status on a read-only cache: %+v", st)
	}
	if l, err := LibDir(); err != nil || l != lib {
		t.Fatalf("LibDir: %q %v", l, err)
	}
	if got := strings.Join(list(), "\n"); got != strings.Join(before, "\n") {
		t.Fatalf("read paths created files:\n%s", got)
	}
	// And the seam is live: a fetch fails on its first write.
	mkdirAll = func(string, os.FileMode) error { return errors.New("read-only cache") }
	if err := Fetch(context.Background(), m.ID, nil); err == nil || !strings.Contains(err.Error(), "read-only cache") {
		t.Fatalf("fetch through the seam: %v", err)
	}
	if got := strings.Join(list(), "\n"); got != strings.Join(before, "\n") {
		t.Fatalf("fetch created files:\n%s", got)
	}
}

// A second fetch waits for the first one's lock, and gives up when ctx is done.
func TestFetchLockWaitRespectsContext(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		lock.With(filepath.Join(Dir(), ".fetch.lock"), func() error { close(held); <-release; return nil })
	}()
	<-held
	defer func() { close(release); <-done }()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	p := testPins(t, srv, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	start := time.Now()
	err := fetch(ctx, p, srv.Client(), "m", &out)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	if !strings.Contains(out.String(), "waiting for another model fetch") {
		t.Fatalf("no waiting notice: %q", out.String())
	}
}

// A platform without a pinned llama.cpp build (linux-arm64, darwin, windows)
// says so plainly, and does not suggest a fetch that cannot help.
func TestUnpinnedPlatform(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	p := pins()
	q := *p
	q.Llama.Assets = map[string]asset{}
	st := statusOf(&q, Default().ID)
	if st.LibOK || !strings.Contains(st.Problem, "no prebuilt llama.cpp is pinned for "+platformKey()) ||
		!strings.Contains(st.Problem, "keyword search still works") || strings.Contains(st.Problem, "ct vault model fetch") {
		t.Fatalf("problem: %q", st.Problem)
	}
	if err := fetch(context.Background(), &q, nil, Default().ID, nil); err == nil || !strings.Contains(err.Error(), "no prebuilt llama.cpp") {
		t.Fatalf("fetch: %v", err)
	}
}
