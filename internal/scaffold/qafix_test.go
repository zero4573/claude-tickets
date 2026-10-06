package scaffold

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

// TestApplyWaitsForAnotherRun: the lock is per run, so a run waits while
// another update or init holds it, even one with the same (default)
// owner name; Acquire would let a holder of the same name straight in.
func TestApplyWaitsForAnotherRun(t *testing.T) {
	for _, o := range []Options{{Version: "1.0"}, {Version: "1.0", AddOnly: true, Owner: "ct-vault-init"}} {
		held := o.Owner
		if held == "" {
			held = "ct-vault-update"
		}
		src, v := applySetup(t)
		if err := vaultlock.Acquire(v, held); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := Apply(v, src, o)
			done <- err
		}()
		time.Sleep(300 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatalf("%s: didn't wait for another run's lock (err %v)", held, err)
		default:
		}
		if _, err := os.Stat(filepath.Join(v, "missing.md")); err == nil {
			t.Fatalf("%s: wrote while another run held the lock", held)
		}
		if err := vaultlock.Release(v, held); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s: didn't take the released lock", held)
		}
		if _, err := os.Stat(filepath.Join(v, ".vault.lock.d")); err == nil {
			t.Errorf("%s: the lock wasn't released", held)
		}
	}
}

// TestWriteAtomicConcurrent: writes to one file at once each use a temp
// name of their own, so none fails or leaves a temp file behind, and a
// temp file another run left is never touched.
func TestWriteAtomicConcurrent(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, ".a.md.ct-tmp")
	if err := os.WriteFile(other, []byte("another run's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() { errs <- writeAtomic(filepath.Join(dir, "a.md"), []byte("x\n"), 0, false) }()
	}
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if read(t, dir, "a.md") != "x\n" || read(t, dir, ".a.md.ct-tmp") != "another run's\n" {
		t.Error("wrong contents")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".a.md.*.ct-tmp"))
	if len(left) > 0 {
		t.Error("temp files left:", left)
	}
}

// TestApplyKeepsMode: an updated or taken file keeps its mode; a new one
// gets 0644 (less the umask).
func TestApplyKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes on Windows")
	}
	src, v := applySetup(t)
	for _, rel := range []string{"stale.md", "edited.md"} {
		if err := os.Chmod(filepath.Join(v, rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(v, "current.md"), 0o640); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, src, Options{Version: "1.0", Take: []string{"edited.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done["stale.md"] != Updated || res.Done["edited.md"] != Taken {
		t.Fatalf("done %v", res.Done)
	}
	mode := func(rel string) os.FileMode {
		st, err := os.Stat(filepath.Join(v, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return st.Mode().Perm()
	}
	for rel, want := range map[string]os.FileMode{"stale.md": 0o600, "edited.md": 0o600, "edited.md.bak": 0o600, "current.md": 0o640} {
		if got := mode(rel); got != want {
			t.Errorf("%s: mode %o, want %o", rel, got, want)
		}
	}
	if got := mode("missing.md"); got&^0o644 != 0 || got&0o600 != 0o600 {
		t.Errorf("a new file: mode %o, want 0644 less the umask", got)
	}
	if !strings.HasPrefix(read(t, v, "stale.md"), "new") {
		t.Error("not updated")
	}
}
