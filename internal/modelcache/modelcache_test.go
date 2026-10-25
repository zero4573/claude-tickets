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

	"github.com/zero4573/claude-tickets/internal/embed"
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
	for _, k := range []string{"linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64"} {
		if _, ok := p.Llama.Assets[k]; !ok {
			t.Errorf("no pin for %s", k)
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
