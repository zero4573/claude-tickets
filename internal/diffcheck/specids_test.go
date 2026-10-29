package diffcheck

import (
	"reflect"
	"testing"
)

func specSrc(ids ...string) []byte {
	s := "---\ncapability: x\nprefix: ORD\n---\n# X\n\n## Requirements\n\n"
	for _, id := range ids {
		s += "### " + id + " · t\nstatus: proposed\nThe system SHALL.\n\n"
	}
	return []byte(s)
}

func TestSpecIDs(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"specs/a/spec.md": specSrc("REQ-ORD-1")})
	DiffCheckGit(t, d, "branch", "topic")
	DiffCheckGit(t, d, "checkout", "-q", "-b", "other")
	DiffCheckCommit(t, d, "other", map[string][]byte{"specs/a/spec.md": specSrc("REQ-ORD-1", "REQ-ORD-2")})
	DiffCheckGit(t, d, "checkout", "-q", "topic")

	// no specs at the end, a single revision, and a clean range give nothing
	for _, r := range []string{"main..topic", "topic", "main...topic"} {
		if h, err := CheckSpecIDs(d, r); err != nil || len(h) != 0 {
			t.Fatalf("%s: %v %v", r, h, err)
		}
	}
	DiffCheckCommit(t, d, "topic", map[string][]byte{"specs/a/spec.md": specSrc("REQ-ORD-1", "REQ-ORD-2")})
	h, err := CheckSpecIDs(d, "other..topic")
	if err != nil || len(h) != 1 || h[0].ID != "REQ-ORD-2" || h[0].Reason != "clash" {
		t.Fatalf("clash: %v %v", h, err)
	}
	if h, err := CheckSpecIDs(d, "other...topic"); err != nil || len(h) != 1 {
		t.Fatalf("three dots: %v %v", h, err)
	}
	if h, err := CheckSpecIDs(d, "main..topic"); err != nil || len(h) != 0 {
		t.Fatalf("clean: %v %v", h, err)
	}
	DiffCheckCommit(t, d, "dup", map[string][]byte{"specs/b/spec.md": specSrc("REQ-ORD-1")})
	h, err = CheckSpecIDs(d, "main..topic")
	var got []string
	for _, x := range h {
		got = append(got, x.Reason+" "+x.ID+" "+x.Path)
	}
	want := []string{"duplicate REQ-ORD-1 specs/a/spec.md", "duplicate REQ-ORD-1 specs/b/spec.md"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("dup: %v %v", got, err)
	}
}
