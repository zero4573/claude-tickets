package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkStatus(t *testing.T) {
	r := Repo{Marks: map[string]Mark{"docs": {Commit: "c", Tree: "t1"}}}
	for _, c := range []struct{ kind, tree, want string }{
		{"docs", "t1", MarkOK}, {"docs", "t2", MarkStale}, {"tidy", "t1", MarkNone},
	} {
		if got := MarkStatus(r, c.kind, c.tree); got != c.want {
			t.Errorf("%s %s: %s, want %s", c.kind, c.tree, got, c.want)
		}
	}
	if MarkStatus(Repo{}, "docs", "t") != MarkNone {
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
	if err := SetMark(file, "s", "docs", Mark{Commit: "c", Tree: "t", At: "now"}); err != nil {
		t.Fatal(err)
	}
	if err := SetMark(file, "x", "docs", Mark{}); err == nil {
		t.Error("unknown slug accepted")
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
	if got.Branch != "b2" || got.Marks["docs"].Tree != "t" {
		t.Errorf("marks lost: %+v", got)
	}
	// a Put that carries marks replaces them
	r.Marks = map[string]Mark{"tidy": {Tree: "n"}}
	if err := Put(file, r); err != nil {
		t.Fatal(err)
	}
	got = read()
	if _, ok := got.Marks["docs"]; ok || got.Marks["tidy"].Tree != "n" {
		t.Errorf("marks not replaced: %+v", got.Marks)
	}
}
