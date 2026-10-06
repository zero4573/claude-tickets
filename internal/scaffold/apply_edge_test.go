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

// TestApplyWaitsForLock: a real run waits while someone else (a
// /tickets:save) holds the vault lock and writes once it's released; a dry
// run neither waits for the lock nor touches it.
func TestApplyWaitsForLock(t *testing.T) {
	src, v := applySetup(t)
	if err := vaultlock.Acquire(v, "PROJ-12"); err != nil {
		t.Fatal(err)
	}
	dir, err := vaultlock.Dir(v)
	if err != nil {
		t.Fatal(err)
	}

	dry := make(chan error, 1)
	go func() {
		_, err := Apply(v, src, Options{DryRun: true})
		dry <- err
	}()
	select {
	case err := <-dry:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a dry run waited for the vault lock")
	}
	if h := vaultlock.Holder(dir); h != "PROJ-12" {
		t.Fatalf("after a dry run the lock is held by %q", h)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Apply(v, src, Options{Version: "1.0"})
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Apply didn't wait for the lock (err %v)", err)
	default:
	}
	if read(t, v, "stale.md") != "old\n" {
		t.Fatal("a file was written while another holder had the lock")
	}
	if err := vaultlock.Release(v, "PROJ-12"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Apply didn't take the released lock")
	}
	if read(t, v, "stale.md") != "new\n" {
		t.Error("the stale file wasn't updated once the lock was free")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the lock wasn't released")
	}
}

// TestApplyErrorNamesFile: a vault path it can't look at (here, a parent
// that's a file) fails the run with the file named, before anything is
// written, and the lock is released.
func TestApplyErrorNamesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path under a file as not found, not ENOTDIR")
	}
	src := source(t, map[string]string{"a.md": "new\n", "t/b.md": "new\n"}, map[string][]string{"a.md": {"old\n"}})
	v := newVault(t, map[string]string{"a.md": "old\n", "t": "a file where a folder should be\n"})
	before := snapshot(t, v)
	for _, dry := range []bool{true, false} {
		_, err := Apply(v, src, Options{DryRun: dry, Version: "1.0"})
		if err == nil || !strings.Contains(err.Error(), filepath.Join("t", "b.md")) {
			t.Fatalf("dry run %v: err %v, want one naming t/b.md", dry, err)
		}
	}
	if after := snapshot(t, v); !sameSnap(before, after) {
		t.Errorf("a failed run changed the vault:\n%v\n%v", before, after)
	}
}

// TestApplyStaleCRLF: an older shipped version saved with CRLF line endings
// and extra trailing newlines is still unedited, so it's updated (with the
// shipped bytes) and recorded.
func TestApplyStaleCRLF(t *testing.T) {
	src := source(t, map[string]string{"a.md": "new\nline\n"}, map[string][]string{"a.md": {"old\nline\n"}})
	v := newVault(t, map[string]string{"a.md": "old\r\nline\r\n\r\n"})
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done["a.md"] != Updated {
		t.Fatalf("done %v, want a.md updated", res.Done)
	}
	if read(t, v, "a.md") != "new\nline\n" {
		t.Errorf("a.md is %q", read(t, v, "a.md"))
	}
	if got := loadRec(t, v).Files["a.md"]; got.SHA256 != Hash([]byte("new\nline\n")) || got.Version != "1.0" {
		t.Errorf("record entry %v", got)
	}
}

// TestApplyRecordOutsideVault: record paths that leave the vault (a
// hand-edited .scaffold.json) are never looked at or reported, and are
// dropped from the record the next time it's written.
func TestApplyRecordOutsideVault(t *testing.T) {
	parent := t.TempDir()
	v := filepath.Join(parent, "v")
	if err := os.MkdirAll(filepath.Join(v, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, parent, "outside.md", "x\n")
	abs := filepath.ToSlash(filepath.Join(parent, "outside.md"))
	write(t, v, RecordName, `{"format": 1, "files": {"../outside.md": {"sha256": "`+Hash([]byte("x\n"))+`", "version": "0"}, "`+abs+`": {"sha256": "`+Hash([]byte("x\n"))+`", "version": "0"}}}`)
	src := source(t, map[string]string{"a.md": "new\n"}, nil)
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Entries {
		if e.Rel != "a.md" {
			t.Errorf("listed %s (%v)", e.Rel, e.State)
		}
	}
	if read(t, parent, "outside.md") != "x\n" {
		t.Error("a file outside the vault changed")
	}
	rec := loadRec(t, v)
	if _, ok := rec.Files["a.md"]; !ok || len(rec.Files) != 1 {
		t.Errorf("record files %v, want only a.md", rec.Files)
	}
}

func sameSnap(a, b snap) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestApplyNewerThenEdited: a file a newer ct wrote and the user then
// edited is edited (kept), its record entry (the newer ct's version) is
// kept; reverting the edit makes it newer again, still never downgraded.
func TestApplyNewerThenEdited(t *testing.T) {
	src := source(t, map[string]string{"a.md": "new\n"}, map[string][]string{"a.md": {"old\n"}})
	v := newVault(t, map[string]string{"a.md": "newer\nmine\n"})
	prev := map[string]Shipped{"a.md": {Hash([]byte("newer\n")), "9.0"}}
	if err := saveRecord(v, prev); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if s := states(res.Entries)["a.md"]; s != Edited || read(t, v, "a.md") != "newer\nmine\n" {
		t.Errorf("edited newer file: state %v, content %q", s, read(t, v, "a.md"))
	}
	if loadRec(t, v).Files["a.md"] != prev["a.md"] {
		t.Error("the newer ct's record entry was dropped or changed")
	}
	write(t, v, "a.md", "newer\r\n")
	res, err = Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if s := states(res.Entries)["a.md"]; s != Newer || read(t, v, "a.md") != "newer\r\n" {
		t.Errorf("reverted newer file: state %v, content %q", s, read(t, v, "a.md"))
	}
}
