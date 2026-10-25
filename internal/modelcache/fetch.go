package modelcache

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/lock"
)

// Timeouts of the default client: the server must answer within
// headerTimeout, and a body that delivers nothing for idleTimeout is cut.
var (
	headerTimeout = 30 * time.Second
	idleTimeout   = 60 * time.Second
)

func newClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: tr}
}

// idleReader resets timer on every read that makes progress.
type idleReader struct {
	r io.Reader
	t *time.Timer
}

func (i idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.t.Reset(idleTimeout)
	}
	return n, err
}

// removeStale deletes what an interrupted fetch left in dir.
func removeStale(dir string) {
	for _, pat := range []string{".download-*", ".stage-*"} {
		old, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, p := range old {
			os.RemoveAll(p)
		}
	}
}

// Fetch downloads the model and this platform's llama.cpp libraries into
// Dir(), verifying their sha256 and installing them atomically (a temp file
// or directory renamed into place). Nothing already present is downloaded
// again. Progress goes to w.
func Fetch(ctx context.Context, id string, w io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	return fetch(ctx, pins(), newClient(), id, w)
}

func fetch(ctx context.Context, p *pinFile, c *http.Client, id string, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	m, ok := modelOf(p, id)
	if !ok {
		return fmt.Errorf("unknown model %q (ct vault model list)", id)
	}
	if _, err := libDirFor(p); err != nil {
		return err
	}
	// One fetch at a time: a second one waits instead of deleting the first
	// one's .download-* and .stage-* in removeStale.
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return lock.With(filepath.Join(Dir(), ".fetch.lock"), func() error { return fetchLocked(ctx, p, c, m, w) })
}

func fetchLocked(ctx context.Context, p *pinFile, c *http.Client, m Model, w io.Writer) error {
	id := m.ID
	st := statusOf(p, id)
	libDir, _ := libDirFor(p)
	removeStale(filepath.Dir(libDir))
	removeStale(filepath.Dir(ModelPath(m)))
	if st.LibOK {
		fmt.Fprintf(w, "llama.cpp %s: present\n", p.Llama.Version)
	} else {
		a := p.Llama.Assets[platformKey()]
		fmt.Fprintf(w, "llama.cpp %s (%s): downloading\n", p.Llama.Version, platformKey())
		if err := fetchLib(ctx, c, a, libDir); err != nil {
			return err
		}
	}
	if st.ModelOK {
		fmt.Fprintf(w, "model %s: present\n", id)
		return nil
	}
	fmt.Fprintf(w, "model %s: downloading\n", id)
	dst := ModelPath(m)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = download(ctx, c, m.URL, m.SHA256, tmp)
	tmp.Close()
	if err != nil {
		return fmt.Errorf("model %s: %w", id, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// download copies url into f and checks its sha256.
func download(ctx context.Context, c *http.Client, url, want string, f *os.File) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := time.AfterFunc(idleTimeout, cancel)
	defer idle.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), idleReader{resp.Body, idle}); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s, pinned %s", url, got, want)
	}
	return nil
}

// fetchLib downloads the release archive, verifies it and extracts only the
// listed files into a staging directory that is renamed to libDir.
func fetchLib(ctx context.Context, c *http.Client, a asset, libDir string) error {
	parent := filepath.Dir(libDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	arc, err := os.CreateTemp(parent, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(arc.Name())
	defer arc.Close()
	if err := download(ctx, c, a.URL, a.SHA256, arc); err != nil {
		return fmt.Errorf("llama.cpp: %w", err)
	}
	if _, err := arc.Seek(0, io.SeekStart); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extract(arc, a, stage); err != nil {
		return fmt.Errorf("llama.cpp: %w", err)
	}
	// A previous partial install (some files, no complete set) is replaced.
	if err := os.RemoveAll(libDir); err != nil {
		return err
	}
	// MkdirTemp makes the stage 0700; the library must stay readable when a
	// sandbox maps another uid.
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	return os.Rename(stage, libDir)
}

// extract writes the files a lists from the tar.gz r into dir.
func extract(r io.Reader, a asset, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	want := map[string][]string{} // member path -> install names
	for _, f := range a.Files {
		want[path.Join(a.Dir, f.Member)] = append(want[path.Join(a.Dir, f.Member)], f.As)
	}
	done := 0
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		names, ok := want[path.Clean(strings.TrimPrefix(h.Name, "./"))]
		if !ok || h.Typeflag != tar.TypeReg {
			continue
		}
		for i, name := range names {
			if i > 0 { // the same member under another name: copy the first
				if err := copyFile(filepath.Join(dir, names[0]), filepath.Join(dir, name)); err != nil {
					return err
				}
				continue
			}
			out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
		done++
	}
	if done != len(want) {
		return fmt.Errorf("the archive lacks %d of the %d pinned files", len(want)-done, len(want))
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
