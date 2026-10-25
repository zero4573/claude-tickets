package scaffold

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

func removedSetup(t *testing.T) (Source, string) {
	t.Helper()
	src := source(t, map[string]string{"inbox.md": "inbox\n"}, map[string][]string{
		"pending.md": {"view\n"}, "done.md": {"view\n"}, "follow-ups.md": {"view\n"}, "other.md": {"x\n"},
	})
	v := newVault(t, map[string]string{
		"pending.md": "view\r\n", "done.md": "view\n", "follow-ups.md": "mine\n", "other.md": "x\n",
	})
	return src, v
}

func TestApplyRemovesUneditedRetiredViews(t *testing.T) {
	src, v := removedSetup(t)
	// the old record holds all of them
	h := Hash([]byte("view\n"))
	if err := saveRecord(v, map[string]Shipped{
		"pending.md": {h, "0.1"}, "done.md": {h, "0.1"}, "follow-ups.md": {h, "0.1"}, "other.md": {Hash([]byte("x\n")), "0.1"},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"inbox.md": Added, "pending.md": Deleted, "done.md": Deleted}
	if !sameDone(res.Done, want) {
		t.Fatalf("done %v, want %v", res.Done, want)
	}
	for _, rel := range []string{"pending.md", "done.md"} {
		if _, err := os.Lstat(filepath.Join(v, rel)); err == nil {
			t.Errorf("%s not removed", rel)
		}
	}
	if read(t, v, "follow-ups.md") != "mine\n" || read(t, v, "other.md") != "x\n" {
		t.Error("an edited view or another retired file was touched")
	}
	rec := loadRec(t, v)
	if _, ok := rec.Files["pending.md"]; ok {
		t.Error("removed file still in the record")
	}
	if _, ok := rec.Files["done.md"]; ok {
		t.Error("removed file still in the record")
	}
	if _, ok := rec.Files["follow-ups.md"]; !ok {
		t.Error("kept edited view lost its record entry")
	}
	st := states(res.Entries)
	if st["follow-ups.md"] != RetiredEdited || st["other.md"] != RetiredClean {
		t.Errorf("states %v", st)
	}
	// a second run: nothing more to do
	res, err = Apply(v, src, Options{Version: "1.0"})
	if err != nil || len(res.Done) != 0 {
		t.Fatalf("second run: %v %v", res.Done, err)
	}
}

func TestApplyRemovedDryRunAddOnlyAndNonRegular(t *testing.T) {
	src, v := removedSetup(t)
	res, err := Apply(v, src, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done["pending.md"] != Deleted || res.Done["done.md"] != Deleted {
		t.Errorf("dry run done %v", res.Done)
	}
	if read(t, v, "pending.md") != "view\r\n" {
		t.Error("dry run removed a file")
	}
	// init never removes
	res, err = Apply(v, src, Options{AddOnly: true})
	if err != nil || res.Done["pending.md"] != "" || read(t, v, "pending.md") != "view\r\n" {
		t.Fatalf("add-only: %v %v", res.Done, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	// a symlink or a folder counts as edited
	if err := os.Remove(filepath.Join(v, "done.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(v, "other.md"), filepath.Join(v, "done.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(v, "pending.md")); err != nil {
		t.Fatal(err)
	}
	write(t, v, "pending.md/inside.md", "x")
	res, err = Apply(v, src, Options{})
	if err != nil || res.Done["pending.md"] != "" || res.Done["done.md"] != "" {
		t.Fatalf("non-regular: %v %v", res.Done, err)
	}
	st := states(res.Entries)
	if st["done.md"] != RetiredEdited || st["pending.md"] != RetiredEdited {
		t.Errorf("states %v", st)
	}
	if read(t, v, "other.md") != "x\n" {
		t.Error("symlink target touched")
	}
}

// Locked: the caller holds the lock; Apply neither takes nor releases it.
func TestApplyLocked(t *testing.T) {
	src, v := removedSetup(t)
	if err := vaultlock.Acquire(v, "me"); err != nil {
		t.Fatal(err)
	}
	dir, _ := vaultlock.Dir(v)
	done := make(chan error, 1)
	go func() { _, err := Apply(v, src, Options{Locked: true}); done <- err }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if h := vaultlock.Holder(dir); h != "me" {
		t.Errorf("lock holder after Apply: %q", h)
	}
	if read(t, v, "inbox.md") != "inbox\n" {
		t.Error("not written")
	}
}

// W6-claude-tickets-S02: a retired view edited after Classify is kept, with
// a warning and its record entry.
func TestApplyKeepsRetiredViewEditedBeforeRemove(t *testing.T) {
	src, v := removedSetup(t)
	h := Hash([]byte("view\n"))
	if err := saveRecord(v, map[string]Shipped{"pending.md": {h, "0.1"}, "done.md": {h, "0.1"}}); err != nil {
		t.Fatal(err)
	}
	beforeRemove = func(abs string) {
		if filepath.Base(abs) == "pending.md" {
			write(t, v, "pending.md", "edited meanwhile\n")
		}
	}
	t.Cleanup(func() { beforeRemove = nil })
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, v, "pending.md") != "edited meanwhile\n" {
		t.Error("an edited view was deleted")
	}
	if _, err := os.Lstat(filepath.Join(v, "done.md")); err == nil {
		t.Error("the unedited view was kept")
	}
	if _, ok := res.Done["pending.md"]; ok {
		t.Errorf("reported as done: %v", res.Done)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "pending.md") {
		t.Errorf("warnings %v", res.Warnings)
	}
	if _, ok := loadRec(t, v).Files["pending.md"]; !ok {
		t.Error("kept view lost its record entry")
	}
}

// W6-claude-tickets-C04: DryRun ignores Locked: no lock, nothing written.
func TestApplyDryRunWithLockedWritesNothing(t *testing.T) {
	src, v := removedSetup(t)
	dir, err := vaultlock.Dir(v)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, src, Options{DryRun: true, Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	if h := vaultlock.Holder(dir); h != "" {
		t.Errorf("lock holder %q", h)
	}
	if _, err := os.Lstat(filepath.Join(v, "inbox.md")); err == nil {
		t.Error("a file was written")
	}
	if _, err := os.Lstat(filepath.Join(v, ".scaffold.json")); err == nil {
		t.Error("the record was written")
	}
	if res.Done["inbox.md"] != Added {
		t.Errorf("done %v", res.Done)
	}
}
