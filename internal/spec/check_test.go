package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []Target {
	t.Helper()
	root := filepath.Join("testdata", "check", name)
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var out []Target
	for _, e := range ents {
		d := filepath.Join(root, e.Name())
		out = append(out, Target{Slug: e.Name(), Dir: d, Specs: filepath.Join(d, "specs")})
	}
	return out
}

func kinds(fs []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range fs {
		m[f.Kind] = append(m[f.Kind], f)
	}
	return m
}

func TestCheckClean(t *testing.T) {
	if fs := Check(fixture(t, "clean"), nil); len(fs) != 0 {
		t.Fatalf("findings: %v", fs)
	}
}

func TestCheckFindings(t *testing.T) {
	fs := Check(fixture(t, "bad"), nil)
	k := kinds(fs)
	if d := k[KindDuplicate]; len(d) != 1 || d[0].ID != "REQ-ORD-1" || !strings.Contains(d[0].File, "orders") {
		t.Errorf("duplicates: %v", d)
	}
	if m := k[KindMarker]; len(m) != 1 || m[0].ID != "REQ-ORD-2" || !strings.Contains(m[0].Message, "who may cancel") {
		t.Errorf("markers: %v", m)
	}
	if m := k[KindMalformed]; len(m) < 2 || !strings.Contains(m[0].Message, "REQ-ORD-3") {
		t.Errorf("malformed: %v", m)
	}
	// REQ-ORD-1 is not cited (REQ-ORD-10 is another ID); REQ-ORD-3 is not cited either
	u := k[KindUntested]
	if len(u) != 2 || u[0].ID != "REQ-ORD-1" || u[1].ID != "REQ-ORD-3" {
		t.Errorf("untested: %v", u)
	}
	// provides without a consumes row (GET /refunds), with a repo without specs (acme-nowhere)
	i := k[KindInterface]
	if len(i) != 2 || !strings.Contains(i[0].Message, "GET /refunds") || !strings.Contains(i[1].Message, "no spec") {
		t.Errorf("interfaces: %v", i)
	}
	for _, f := range fs {
		if f.Repo != "acme-billing" {
			t.Errorf("finding in %s: %v", f.Repo, f)
		}
	}
}

func TestCheckOnlyAndConsumes(t *testing.T) {
	// naming the ledger only: its consumes row is matched against billing's
	// provides row, which exists in the clean fixture and not in the bad one
	if fs := Check(fixture(t, "clean"), []string{"acme-ledger"}); len(fs) != 0 {
		t.Fatalf("clean: %v", fs)
	}
	fs := Check(fixture(t, "bad"), []string{"acme-ledger"})
	if len(fs) != 0 {
		t.Fatalf("bad ledger: %v", fs) // POST /orders is provided by billing
	}
	if fs := Check(fixture(t, "bad"), []string{"acme-billing"}); len(fs) == 0 {
		t.Fatal("no findings for billing")
	}
}

func TestCheckSymlinkedTestIgnored(t *testing.T) {
	dir := t.TempDir()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "x_test.go"), []byte("REQ-ORD-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(dir, "tests")); err != nil {
		t.Skip("no symlinks here: " + err.Error())
	}
	if c, _ := citedIDs(dir, filepath.Join(dir, "specs")); len(c) != 0 {
		t.Fatalf("followed a symlink: %v", c)
	}
}

// A search stopped by a cap reports that once, not every requirement it
// didn't reach as untested.
func TestCheckCapReported(t *testing.T) {
	for _, c := range []struct {
		walk, files int
		want        string
	}{{1, 50000, "1 entries"}, {500000, 0, "0 test files"}} {
		maxWalk, maxTestFiles = c.walk, c.files
		fs := Check(fixture(t, "bad"), []string{"acme-billing"})
		maxWalk, maxTestFiles = 500000, 50000
		k := kinds(fs)
		if len(k[KindUntested]) != 0 {
			t.Errorf("cap %s: untested findings past the cap: %v", c.want, k[KindUntested])
		}
		n := 0
		for _, f := range k[KindMalformed] {
			if strings.Contains(f.Message, "stopped at "+c.want) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("cap %s: %d findings naming it: %v", c.want, n, k[KindMalformed])
		}
	}
}
