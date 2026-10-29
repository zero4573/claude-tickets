package spec

import (
	"os"
	"strings"
	"testing"
)

func diffTree(t *testing.T, side string) Tree {
	t.Helper()
	tr, err := ReadTree("testdata/diff/" + side)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDiffTreesRequirementLevel(t *testing.T) {
	d := DiffTrees("acme-billing", diffTree(t, "base"), diffTree(t, "new"))
	got := strings.Join(d.Summary(), "\n")
	for _, want := range []string{
		"status REQ-ORD-1 · Create (orders, proposed -> implemented)",
		"changed REQ-ORD-2 · Cancel (orders)",
		"removed REQ-ORD-3 · Archive (orders)",
		"added REQ-ORD-4 · Refund (orders, proposed)",
		"added REQ-REF-1 · Refund (refunds, proposed)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	n := d.Counts()
	if n[Added] != 2 || n[Changed] != 1 || n[Removed] != 1 || n[Flipped] != 1 {
		t.Errorf("counts = %v", n)
	}
	if len(d.Files) != 2 || d.Files[0].Path != "orders/spec.md" || d.Files[0].Added == 0 || d.Files[0].Removed == 0 {
		t.Errorf("files = %+v", d.Files)
	}
}

func TestDiffTreesNoChange(t *testing.T) {
	d := DiffTrees("x", diffTree(t, "base"), diffTree(t, "base"))
	if !d.Empty() || len(d.Summary()) != 0 || RenderChange(d) != "" {
		t.Errorf("want empty, got %+v", d)
	}
	if !DiffTrees("x", Tree{}, Tree{}).Empty() {
		t.Error("two empty trees differ")
	}
}

func TestDiffTreesRemovedCapability(t *testing.T) {
	d := DiffTrees("x", diffTree(t, "new"), Tree{})
	if n := d.Counts(); n[Removed] != 4 {
		t.Errorf("counts = %v", n)
	}
}

func TestOverlayLeavesOtherCapabilities(t *testing.T) {
	base, over := diffTree(t, "base"), Tree{"refunds/spec.md": diffTree(t, "new")["refunds/spec.md"]}
	d := DiffTrees("x", base, Overlay(base, over))
	if n := d.Counts(); n[Added] != 1 || n[Removed] != 0 {
		t.Errorf("counts = %v", n)
	}
}

func TestInterfacesChange(t *testing.T) {
	a := []byte("---\ncapability: p\nprefix: PP\n---\n## Requirements\n\n## Interfaces\n\n### Provides\n| a |\n")
	b := []byte("---\ncapability: p\nprefix: PP\n---\n## Requirements\n\n## Interfaces\n\n### Provides\n| a |\n| b |\n")
	d := DiffTrees("x", Tree{"p/spec.md": a}, Tree{"p/spec.md": b})
	if len(d.Caps) != 1 || !strings.Contains(d.Caps[0].Interfaces, "| b |") {
		t.Fatalf("caps = %+v", d.Caps)
	}
}

func TestRenderChangeRoundTrips(t *testing.T) {
	d := DiffTrees("acme-billing", diffTree(t, "base"), diffTree(t, "new"))
	body := RenderChange(d)
	if !strings.Contains(body, "### acme-billing · orders\n") || !strings.Contains(body, "Removed: REQ-ORD-3") {
		t.Fatalf("body:\n%s", body)
	}
	cs := ParseChange("desc", []byte("## Spec change\n\n"+body))
	if len(cs.Problems) != 0 || len(cs.Changes) != 2 {
		t.Fatalf("problems %v, changes %d", cs.Problems, len(cs.Changes))
	}
	var ids []string
	for _, r := range cs.Requirements() {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, " "); got != "REQ-ORD-1 REQ-ORD-2 REQ-ORD-4 REQ-REF-1" {
		t.Errorf("ids = %s", got)
	}
}

func TestWriteTreeRoundTrips(t *testing.T) {
	root := t.TempDir() + "/base/x"
	src := diffTree(t, "new")
	if err := WriteTree(root, src); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if !DiffTrees("x", src, got).Empty() || len(got) != len(src) {
		t.Errorf("round trip differs: %d vs %d files", len(got), len(src))
	}
	empty := t.TempDir() + "/e"
	if err := WriteTree(empty, Tree{}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(empty); err != nil || !st.IsDir() {
		t.Errorf("empty tree gave no folder: %v", err)
	}
}
