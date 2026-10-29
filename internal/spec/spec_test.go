package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, rel string) *Spec {
	t.Helper()
	s, err := ParseFile(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGood(t *testing.T) {
	s := parse(t, "good/order-sync/spec.md")
	if len(s.Problems) != 0 {
		t.Fatalf("problems: %v", s.Problems)
	}
	if s.Capability != "order-sync" || s.Prefix != "ORD" || len(s.Requirements) != 2 {
		t.Fatalf("got %+v", s)
	}
	r := s.Requirements[0]
	if r.ID != "REQ-ORD-14" || r.Title != "Sync retries" || r.Status != Implemented || r.Num != 14 || r.Line != 12 {
		t.Fatalf("req %+v", r)
	}
	if len(r.Scenarios) != 1 || r.Scenarios[0].Name != "transient failure" || len(r.Scenarios[0].Steps) != 3 || r.Scenarios[0].Steps[2].Kind != "AND" {
		t.Fatalf("scenarios %+v", r.Scenarios)
	}
	if !strings.HasPrefix(r.Statement, "The system SHALL retry") {
		t.Fatalf("statement %q", r.Statement)
	}
	i := s.Requirements[1]
	if i.Status != Inferred || len(i.Evidence) != 2 || len(i.Tests) != 1 || i.Evidence[1] != "src/cancel.go:40" {
		t.Fatalf("inferred %+v", i)
	}
}

func TestBroken(t *testing.T) {
	s := parse(t, "broken/bad/spec.md")
	want := []string{
		"spec.md:9: malformed requirement ID",
		"spec.md:14: REQ-BAD-1: the line after the heading must be",
		"spec.md:21: REQ-BAD-2: status must be",
		"REQ-BAD-3: statement has no SHALL",
		"REQ-BAD-3: scenario \"s\" must be WHEN",
		"duplicate ID REQ-BAD-3",
		"not a requirement",
		"REQ-OTH-4 does not use the spec's prefix BAD",
	}
	var all []string
	for _, p := range s.Problems {
		if p.File == "" || p.Line < 1 {
			t.Errorf("problem without position: %+v", p)
		}
		all = append(all, p.String())
	}
	joined := strings.Join(all, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("missing problem %q in:\n%s", w, joined)
		}
	}
}

func TestMarkers(t *testing.T) {
	s := parse(t, "markers/m/spec.md")
	if len(s.Problems) != 0 {
		t.Fatalf("problems: %v", s.Problems)
	}
	a, b := s.Requirements[0], s.Requirements[1]
	if !a.Open() || len(a.Markers) != 2 || a.Markers[0].Question != "after how long?" || a.Markers[1].Line != 13 {
		t.Fatalf("markers %+v", a.Markers)
	}
	if b.Open() {
		t.Fatal("closed requirement reports markers")
	}
	if strings.Contains(a.Statement, "NEEDS") {
		t.Fatalf("marker leaked into statement: %q", a.Statement)
	}
}

func TestBugThreePart(t *testing.T) {
	b, _ := os.ReadFile("testdata/bug/b/description.md")
	cs := ParseChange("description.md", b)
	if !cs.Found || cs.Line != 3 || len(cs.Problems) != 0 {
		t.Fatalf("%+v", cs)
	}
	if len(cs.Changes) != 1 || cs.Changes[0].Slug != "billing" || cs.Changes[0].Capability != "order-sync" {
		t.Fatalf("changes %+v", cs.Changes)
	}
	reqs := cs.Requirements()
	if len(reqs) != 1 || reqs[0].ID != "REQ-ORD-20" {
		t.Fatalf("reqs %+v", reqs)
	}
	p := reqs[0].Parts
	if p["Current"] != "Retries charge twice." || p["Expected"] != "A retry never charges twice." || p["Unchanged"] != "Failed orders stay failed." {
		t.Fatalf("parts %+v", p)
	}
	if !strings.Contains(cs.Changes[0].Interfaces, "POST /charge") {
		t.Fatalf("interfaces %q", cs.Changes[0].Interfaces)
	}
}

func TestChangeSectionMissing(t *testing.T) {
	cs := ParseChange("d.md", []byte("# T\n\n## Spec changes\n### a · b\n"))
	if cs.Found {
		t.Fatal("renamed heading must count as missing")
	}
}

func TestChangeSectionProblems(t *testing.T) {
	cs := ParseChange("d.md", []byte("## Spec change\n### REQ-ORD-1 · T\nstatus: proposed\nThe system SHALL.\n\n### nodot\n"))
	if len(cs.Problems) != 2 || cs.Problems[0].Line != 2 || cs.Problems[1].Line != 6 {
		t.Fatalf("%v", cs.Problems)
	}
}

func TestNoPanicOnGarbage(t *testing.T) {
	for _, src := range []string{"", "---", "---\n", "### REQ-A-1 · x", "## Requirements\n### REQ-", "\ufeff---\nprefix: A\n---\n## Requirements\n###", "## Spec change\n#### Interfaces\n### REQ-AB-1 · x\n#### Scenario:\n"} {
		Parse("g.md", []byte(src))
		ParseChange("g.md", []byte(src))
		FindIDs("g.md", []byte(src))
	}
}

func TestFindIDsAndDuplicates(t *testing.T) {
	src := "### REQ-AB-1 · a\n```\n### REQ-AB-9 · in fence\n```\n### REQ-AB-2 · b\n### REQ-AB-1 · again\n### REQ-A-3 · bad\n"
	ids := FindIDs("f.md", []byte(src))
	if len(ids) != 3 || ids[2].Line != 6 {
		t.Fatalf("%+v", ids)
	}
	d := Duplicates(ids)
	if len(d) != 1 || len(d["REQ-AB-1"]) != 2 || SortedIDs(d)[0] != "REQ-AB-1" {
		t.Fatalf("%+v", d)
	}
}

func TestCapabilities(t *testing.T) {
	caps, err := Capabilities("testdata/repo/specs")
	if err != nil || len(caps) != 2 || caps[0].Name != "alpha" || caps[1].Name != "beta" {
		t.Fatalf("%v %v", caps, err)
	}
	none, err := Capabilities("testdata/nope")
	if err != nil || len(none) != 0 {
		t.Fatalf("%v %v", none, err)
	}
	ids, err := RootIDs("testdata/good")
	if err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestResolve(t *testing.T) {
	vault, repo := t.TempDir(), t.TempDir()
	note := func(fm string) {
		p := ProjectNote(vault, "acme-app")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("---\ntype: project\n"+fm+"---\n"), 0o644)
	}
	note("")
	if l, err := Resolve(vault, "acme-app", repo); err != nil || l.Kind != Unset {
		t.Fatalf("%+v %v", l, err)
	}
	os.Mkdir(filepath.Join(repo, "specs"), 0o755)
	if l, _ := Resolve(vault, "acme-app", repo); l.Kind != InRepo || l.Dir != filepath.Join(repo, "specs") || l.Field != "" {
		t.Fatalf("%+v", l)
	}
	note("specs: vault\n")
	if l, _ := Resolve(vault, "acme-app", repo); l.Kind != InVault || l.Dir != VaultDir(vault, "acme-app") {
		t.Fatalf("%+v", l)
	}
	note("specs: \"repo\"\n")
	if l, _ := Resolve(vault, "acme-app", repo); l.Kind != InRepo {
		t.Fatalf("%+v", l)
	}
	note("specs: elsewhere\n")
	if _, err := Resolve(vault, "acme-app", repo); err == nil {
		t.Fatal("want error")
	}
}

func TestCapabilitiesSkipSymlinks(t *testing.T) {
	d := t.TempDir()
	outside := filepath.Join(d, "outside")
	root := filepath.Join(d, "specs")
	for _, p := range []string{filepath.Join(outside, "x"), filepath.Join(root, "real"), filepath.Join(root, "linkedfile")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	spec := []byte("---\ncapability: x\nprefix: X\n---\n")
	for _, f := range []string{filepath.Join(outside, "x", "spec.md"), filepath.Join(root, "real", "spec.md")} {
		if err := os.WriteFile(f, spec, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "x"), filepath.Join(root, "linkeddir")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "x", "spec.md"), filepath.Join(root, "linkedfile", "spec.md")); err != nil {
		t.Fatal(err)
	}
	caps, err := Capabilities(root)
	if err != nil || len(caps) != 1 || caps[0].Name != "real" {
		t.Fatalf("want only the real capability, got %v %v", caps, err)
	}
	if _, err := ParseFile(filepath.Join(root, "linkedfile", "spec.md")); err == nil {
		t.Fatal("ParseFile followed a symlinked spec.md")
	}
	linkedRoot := filepath.Join(d, "specs2")
	if err := os.Symlink(root, linkedRoot); err != nil {
		t.Fatal(err)
	}
	if caps, err := Capabilities(linkedRoot); err != nil || len(caps) != 0 {
		t.Fatalf("a symlinked specs root was read: %v %v", caps, err)
	}
}

func TestSpecSizeCap(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(d, "big", "spec.md")
	if err := os.WriteFile(f, []byte(strings.Repeat("a\n", MaxSpecSize/2+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(f); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("ParseFile read an oversize spec.md: %v", err)
	}
	if _, err := RootIDs(d); err == nil {
		t.Fatal("RootIDs read an oversize spec.md")
	}
}
