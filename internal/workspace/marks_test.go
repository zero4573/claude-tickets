package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkStatus(t *testing.T) {
	r := Repo{Marks: map[string]Mark{"tidy": {Commit: "c", Tree: "t1"}, "docs": {Commit: "c", Tree: "t1"}}}
	for _, c := range []struct{ kind, tree, want string }{
		{"tidy", "t1", MarkOK}, {"tidy", "t2", MarkStale}, {"other", "t1", MarkNone},
	} {
		if got := MarkStatus(r, c.kind, c.tree); got != c.want {
			t.Errorf("%s %s: %s, want %s", c.kind, c.tree, got, c.want)
		}
	}
	if MarkStatus(Repo{}, "tidy", "t") != MarkNone {
		t.Error("no marks should be none")
	}
}

func TestSetMarkAndPutKeepsMarks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "workspace.json")
	v := "1"
	r := Repo{Slug: "s", Path: filepath.Join(dir, "s"), Branch: "b", TargetVersion: &v}
	if err := Put(file, r); err != nil {
		t.Fatal(err)
	}
	if err := SetMark(file, "s", "tidy", Mark{Commit: "c", Tree: "t", At: "now"}); err != nil {
		t.Fatal(err)
	}
	if err := SetMark(file, "x", "tidy", Mark{}); err == nil {
		t.Error("unknown slug accepted")
	}
	if err := SetMark(file, "s", "docs", Mark{}); err == nil {
		t.Error("docs kind accepted")
	}
	if err := SetMark(file, "s", "other", Mark{}); err == nil {
		t.Error("unknown kind accepted")
	}
	// Put of the same slug without marks keeps them
	r.Branch = "b2"
	if err := Put(file, r); err != nil {
		t.Fatal(err)
	}
	read := func() Repo {
		os.MkdirAll(r.Path, 0o755)
		info, err := Read(dir)
		if err != nil || len(info.Repos) != 1 {
			t.Fatalf("read: %v %v", info, err)
		}
		return info.Repos[0]
	}
	got := read()
	if got.Branch != "b2" || got.Marks["tidy"].Tree != "t" {
		t.Errorf("marks lost: %+v", got)
	}
	// a Put that carries marks replaces them
	r.Marks = map[string]Mark{"docs": {Tree: "n"}}
	if err := Put(file, r); err != nil {
		t.Fatal(err)
	}
	got = read()
	if _, ok := got.Marks["tidy"]; ok || got.Marks["docs"].Tree != "n" {
		t.Errorf("marks not replaced: %+v", got.Marks)
	}
}

// An old docs entry is kept in the file but never counts: the status of the
// tidy kind alone decides, and ValidMark refuses docs.
func TestOldDocsMarkIgnored(t *testing.T) {
	if ValidMark("docs") {
		t.Error("docs is no longer a mark kind")
	}
	r := Repo{Marks: map[string]Mark{"docs": {Tree: "t"}}}
	for _, k := range MarkKinds {
		if MarkStatus(r, k, "t") != MarkNone {
			t.Errorf("kind %s should be none with only a docs entry", k)
		}
	}
}
