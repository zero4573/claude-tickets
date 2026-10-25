package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDiff(t *testing.T) {
	v := t.TempDir()
	snap, err := snapshotTickets(v)
	if err != nil || len(snap) != 0 {
		t.Fatalf("missing tickets/: %v %v", snap, err)
	}
	tk := filepath.Join(v, "tickets")
	write(t, filepath.Join(tk, "A/A.md"), "aaa")
	write(t, filepath.Join(tk, "B/B.md"), "bbb")
	write(t, filepath.Join(tk, "C/C.md"), "ccc")
	write(t, filepath.Join(tk, "D/D.md"), "ddd")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(tk, "D/D.md"), old, old)
	before, err := snapshotTickets(v)
	if err != nil || before["tickets/A/A.md"].size != 3 {
		t.Fatalf("%v %v", before, err)
	}
	if got := changedPaths(before, before); len(got) != 0 {
		t.Fatalf("unchanged: %v", got)
	}
	write(t, filepath.Join(tk, "A/A.md"), "longer") // edit
	write(t, filepath.Join(tk, "E/E.md"), "new")    // add
	os.Remove(filepath.Join(tk, "B/B.md"))          // remove
	now := time.Now().Add(time.Hour)                // same size, new mtime
	os.Chtimes(filepath.Join(tk, "D/D.md"), now, now)
	after, err := snapshotTickets(v)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tickets/A/A.md", "tickets/B/B.md", "tickets/D/D.md", "tickets/E/E.md"}
	if got := changedPaths(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSnapshotUnreadable(t *testing.T) {
	v := t.TempDir()
	write(t, filepath.Join(v, "tickets"), "a file, not a dir")
	if _, err := snapshotTickets(v); err == nil {
		t.Fatal("want an error")
	}
}

func TestRefreshAfterSync(t *testing.T) {
	var calls [][]string
	orig := refreshIndex
	defer func() { refreshIndex = orig }()
	refreshIndex = func(_ string, files ...string) { calls = append(calls, files) }

	v := t.TempDir()
	p := filepath.Join(v, "tickets/A/A.md")
	write(t, p, "x")
	before, _ := snapshotTickets(v)

	refreshAfterSync(v, before, nil, "")
	if len(calls) != 0 {
		t.Fatalf("empty diff must not refresh: %v", calls)
	}
	write(t, p, "xyz")
	refreshAfterSync(v, before, nil, "inbox/ticket-sync.md")
	if want := []string{"tickets/A/A.md", "inbox/ticket-sync.md"}; len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("got %v", calls)
	}
	calls = nil
	refreshAfterSync(v, nil, errors.New("boom"), "inbox/n.md")
	if len(calls) != 1 || len(calls[0]) != 0 {
		t.Fatalf("snapshot failure: want one whole-vault call, got %v", calls)
	}
	// note only, nothing else changed
	calls = nil
	before, _ = snapshotTickets(v)
	refreshAfterSync(v, before, nil, "inbox/n.md")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], []string{"inbox/n.md"}) {
		t.Fatalf("got %v", calls)
	}
}
