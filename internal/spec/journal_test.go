package spec

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJournalRoundTrip(t *testing.T) {
	ops := []Op{
		{Kind: OpReplaceTree, Arg: TreeBase},
		{Kind: OpWrite, Arg: WriteOfKept, Dest: "tickets/manual/T-1/specs-after/.acme.kept"},
		{Kind: OpTrash, Arg: "a/spec.md", Hash: Hash([]byte("a"))},
		{Kind: OpWrite, Arg: WriteOfNote, Dest: "projects/acme/spec-versions/acme spec T-1.md"},
		{Kind: OpReplaceTree, Arg: TreeAfter},
		{Kind: OpPut, Arg: "b c/spec.md", Hash: PreAbsent},
		{Kind: OpPut, Arg: "d/spec.md", Hash: Hash([]byte("d"))},
		{Kind: OpDrop, Arg: "b c/spec.md", Hash: Hash([]byte("b"))},
	}
	got, err := ParseJournal(RenderJournal(ops))
	if err != nil || !reflect.DeepEqual(got, ops) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	h := Hash([]byte("x"))
	for _, bad := range []string{
		"move a/spec.md", "replace-tree copy", "write other x.md", "write note",
		"put ../x.md " + h, "drop /etc/x " + h, "trash a/../../x " + h, "put  " + h, "put a\\b.md " + h, "put ./a.md " + h,
		"put a.md", "put a.md sha256:abc", "put a.md " + strings.ToUpper(h), "put a.md sha256:" + strings.Repeat("g", 64),
		"trash a.md absent", "drop a.md absent", "put a\x01b.md " + h, "put a\rb.md " + h,
		"drop a.md " + h + "\nput b.md " + h,
	} {
		if _, err := ParseJournal([]byte(JournalHeader + "\n" + bad + "\n")); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	// the hash is the last token: the path may hold spaces
	ops, err = ParseJournal([]byte(JournalHeader + "\nput a b.md " + h + "\n"))
	if err != nil || len(ops) != 1 || ops[0].Arg != "a b.md" || ops[0].Hash != h {
		t.Fatalf("put with a space: %+v %v", ops, err)
	}
	for _, bad := range []string{"", "put a.md " + h + "\n", "promote-journal v1\nput a.md " + h + "\n"} {
		if _, err := ParseJournal([]byte(bad)); err == nil {
			t.Errorf("no v2 header %q: want an error", bad)
		}
	}
}

func TestParseDone(t *testing.T) {
	for _, c := range []struct {
		in       string
		done     []int
		complete int
	}{
		{"", nil, 0},
		{"1\n3\n1\n", []int{1, 3}, 6},
		{"1", nil, 0},
		{"2\n1", []int{2}, 2},
		{"12\n", []int{12}, 3},
	} {
		done, n, err := ParseDone([]byte(c.in), 12)
		if err != nil || n != c.complete || len(done) != len(c.done) {
			t.Errorf("%q: %v %d %v", c.in, done, n, err)
			continue
		}
		for _, d := range c.done {
			if !done[d] {
				t.Errorf("%q: %d not done", c.in, d)
			}
		}
	}
	for _, bad := range []string{"x\n", "0\n", "01\n", "+1\n", "-1\n", "13\n", "\n", "1\n\n", " 1\n", "x", "0", "1\nx"} {
		if _, _, err := ParseDone([]byte(bad), 12); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestParseHeldAndAbsent(t *testing.T) {
	if got, err := ParseHeld([]byte("a/spec.md\nb c/spec.md\n")); err != nil || !reflect.DeepEqual(got, []string{"a/spec.md", "b c/spec.md"}) {
		t.Fatalf("held: %v %v", got, err)
	}
	for _, bad := range []string{"../a\n", "/a\n", "a\x01\n"} {
		if _, err := ParseHeld([]byte(bad)); err == nil {
			t.Errorf("held %q: want an error", bad)
		}
	}
	b := RenderAbsent([]string{"a/spec.md"}, []string{"a/spec.md", "b/spec.md"}, "projects/acme/spec-versions/acme-spec-T-1.md")
	got, err := ParseAbsent(b)
	if err != nil || !got[TreeBase]["a/spec.md"] || !got[TreeAfter]["b/spec.md"] || !got[WriteOfNote]["projects/acme/spec-versions/acme-spec-T-1.md"] || got[TreeBase]["b/spec.md"] {
		t.Fatalf("absent: %v %v", got, err)
	}
	for _, bad := range []string{"moves a\n", "base ../a\n", "base\n"} {
		if _, err := ParseAbsent([]byte(bad)); err == nil {
			t.Errorf("absent %q: want an error", bad)
		}
	}
}

func TestDamagedJournal(t *testing.T) {
	v := t.TempDir()
	p := JournalPath(v, "T-1", "acme")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("put a.md absent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, err := ReadJournal(v, "T-1", "acme")
	if ok || !errors.Is(err, ErrJournalDamaged) {
		t.Fatalf("damaged: %v %v", ok, err)
	}
	for _, want := range []string{"acme.journal", filepath.Join("specs-stage", "acme") + string(filepath.Separator), "acme.journal.done", "discards the pending promote"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q lacks %q", err, want)
		}
	}
}

func TestPendingPromote(t *testing.T) {
	v := t.TempDir()
	if err := os.MkdirAll(filepath.Join(v, "tickets", "manual", "T-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v, "tickets", "manual", "T-1", "T-1.md"), []byte("# T-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Pending(v, "T-1", "acme"); err != nil {
		t.Fatalf("no journal: %v", err)
	}
	if _, ok, err := ReadJournal(v, "T-1", "acme"); ok || err != nil {
		t.Fatalf("ReadJournal without one: %v %v", ok, err)
	}
	p := JournalPath(v, "T-1", "acme")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, RenderJournal([]Op{{Kind: OpPut, Arg: "a/spec.md", Hash: PreAbsent}}), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Pending(v, "T-1", "acme")
	if !errors.Is(err, ErrPromotePending) || errors.Is(err, ErrRebasePending) || err.Error() != "acme: promote pending: run ct spec promote T-1" {
		t.Fatalf("pending: %v", err)
	}
	if n := PendingNote(err, "T-1", "acme"); n != PromotePendingNote("T-1") {
		t.Fatalf("note: %q", n)
	}
	if PendingNote(errors.New("other"), "T-1", "acme") != "" || Pending(v, "T-1", "other") != nil {
		t.Fatal("another error or slug reads as pending")
	}
	if ops, ok, err := ReadJournal(v, "T-1", "acme"); !ok || err != nil || len(ops) != 1 {
		t.Fatalf("ReadJournal: %v %v %v", ops, ok, err)
	}
}
