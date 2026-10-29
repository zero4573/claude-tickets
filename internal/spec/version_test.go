package spec

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/note"
)

func TestVersionRoundTrip(t *testing.T) {
	vault := t.TempDir()
	p := VersionPath(vault, "acme-orders", "PROJ-12")
	if want := filepath.Join(vault, "projects", "acme-orders", "spec-versions", "acme-orders-spec-PROJ-12.md"); p != want {
		t.Fatalf("path %s, want %s", p, want)
	}
	v := Version{Slug: "acme-orders", Ticket: "[[PROJ-12]]", Project: "[[acme-orders]]",
		Promoted: "2026-10-29T14:03:11", Files: []string{"billing/spec.md", "orders/spec.md"}, Branch: "feature/proj-12"}
	before := Tree{"orders/spec.md": []byte(specOrders("proposed"))}
	after := Tree{"orders/spec.md": []byte(specOrders("implemented"))}
	d := DiffTrees("acme-orders", before, after)
	b, err := RenderVersion(v, "tickets/jira/PROJ-12", before, d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadVersion(p)
	if err != nil {
		t.Fatal(err)
	}
	v.Type = VersionType
	v.Tags = []string{}
	if !reflect.DeepEqual(got, v) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, v)
	}
	if got.ID() != "PROJ-12" {
		t.Fatalf("ID %q", got.ID())
	}
	body := string(b)
	for _, s := range []string{
		"type: spec-version\n",
		"files: [billing/spec.md, orders/spec.md]\n",
		"Unpinned: promoted, not pushed yet\n",
		"- orders: [[tickets/jira/PROJ-12/specs-base/acme-orders/orders/spec|before]] · [[tickets/jira/PROJ-12/specs-after/acme-orders/orders/spec|after]]\n",
		"## Change\n\n### acme-orders · orders\n",
		"### REQ-ORD-1 · Create\nstatus: implemented\n",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("note lacks %q:\n%s", s, body)
		}
	}
	// a capability without a before file links only its after side
	d2 := DiffTrees("acme-orders", Tree{}, after)
	b2, _ := RenderVersion(v, "tickets/jira/PROJ-12", Tree{}, d2)
	if strings.Contains(string(b2), "|before]]") {
		t.Errorf("new capability links a before file:\n%s", b2)
	}

	list, err := ListVersions(vault, "acme-orders")
	if err != nil || len(list) != 1 || list[0].Path != p {
		t.Fatalf("list: %v %+v", err, list)
	}
	if _, err := ReadVersion(filepath.Join(vault, "nope.md")); !os.IsNotExist(err) {
		t.Fatalf("missing note: %v", err)
	}
	if l, err := ListVersions(vault, "none"); err != nil || l != nil {
		t.Fatalf("no folder: %v %v", l, err)
	}
}

func TestListVersionsOrder(t *testing.T) {
	vault := t.TempDir()
	for _, v := range []Version{
		{Slug: "s", Ticket: "[[A-1]]", Promoted: "2026-01-01T00:00:00"},
		{Slug: "s", Ticket: "[[A-3]]", Promoted: "2026-03-01T00:00:00"},
		{Slug: "s", Ticket: "[[A-2]]", Promoted: "2026-02-01T00:00:00"},
	} {
		b, _ := RenderVersion(v, "tickets/manual/"+v.ID(), nil, Diff{})
		p := VersionPath(vault, "s", v.ID())
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(VersionsDir(vault, "s"), "other.md"), []byte("---\ntype: note\n---\n"), 0o644)
	list, _ := ListVersions(vault, "s")
	var ids []string
	for _, f := range list {
		ids = append(ids, f.Version.ID())
	}
	if strings.Join(ids, " ") != "A-3 A-2 A-1" {
		t.Fatalf("order %v", ids)
	}
}

func TestVersionState(t *testing.T) {
	v := Version{Branch: "feature/x"}
	if s := VersionState(v); s != "Unpinned: promoted, not pushed yet" {
		t.Fatal(s)
	}
	v.Commit, v.Merged, v.Tags = "9f3c2a1aaaaaaa", "4b1d0e2bbbbbbb", []string{"v1.4.0", "v1.5.0"}
	if s := VersionState(v); s != "Pinned to 9f3c2a1 on feature/x, merged as 4b1d0e2, in v1.4.0" {
		t.Fatal(s)
	}
	v.Pinned = "2026-10-29T00:00:00"
	v.ClearPin()
	if v.Commit != "" || v.Pinned != "" || v.Merged != "" || v.Tags != nil {
		t.Fatalf("ClearPin: %+v", v)
	}
}

func TestPinStale(t *testing.T) {
	old := Tree{"a/spec.md": []byte("1"), "b/spec.md": []byte("2")}
	if PinStale(old, Tree{"a/spec.md": []byte("1"), "b/spec.md": []byte("changed")}, []string{"a/spec.md"}) {
		t.Error("a change outside files clears the pin")
	}
	if !PinStale(old, Tree{"a/spec.md": []byte("x"), "b/spec.md": []byte("2")}, []string{"a/spec.md"}) {
		t.Error("a change in files keeps the pin")
	}
	if !PinStale(old, Tree{"b/spec.md": []byte("2")}, []string{"a/spec.md"}) {
		t.Error("a removed file keeps the pin")
	}
	if PinStale(old, old, []string{"a/spec.md", "b/spec.md"}) {
		t.Error("the same tree clears the pin")
	}
}

func TestSides(t *testing.T) {
	f := func(s string) []byte { return []byte(s) }
	projects := Tree{"a/spec.md": f("a-theirs"), "b/spec.md": f("b-ours"), "c/spec.md": f("c-theirs")}
	base := Tree{"a/spec.md": f("a0"), "b/spec.md": f("b0"), "c/spec.md": f("c0")}
	after := Tree{"a/spec.md": f("a0"), "b/spec.md": f("b-ours"), "c/spec.md": f("c0")}
	cp := Tree{"c/spec.md": f("c-copy")}
	for _, tc := range []struct {
		name          string
		base, after   Tree
		before, aftsd Tree
	}{
		{"no base", nil, nil, projects, Overlay(projects, cp)},
		{"base and after", base, after, base, Tree{"a/spec.md": f("a0"), "b/spec.md": f("b-ours"), "c/spec.md": f("c-copy")}},
		{"base only", base, nil, base, Overlay(projects, cp)},
	} {
		b, a := Sides(projects, tc.base, tc.after, cp, []string{"b/spec.md"})
		if !reflect.DeepEqual(b, tc.before) || !reflect.DeepEqual(a, tc.aftsd) {
			t.Errorf("%s: before %q after %q", tc.name, b, a)
		}
	}
	// started, nothing promoted: the copy against its start, other files and
	// other tickets' changes since on both sides alike
	b, a := Sides(projects, base, nil, cp, nil)
	want := Tree{"a/spec.md": f("a-theirs"), "b/spec.md": f("b-ours"), "c/spec.md": f("c0")}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("started: before %q", b)
	}
	if !reflect.DeepEqual(a, Overlay(projects, cp)) {
		t.Errorf("started: after %q", a)
	}
}

func TestPickMerge(t *testing.T) {
	tr := Tree{"a": []byte("1"), "b": []byte("2")}
	if got := Pick(tr, []string{"b", "c"}); !reflect.DeepEqual(got, Tree{"b": []byte("2")}) {
		t.Fatal(got)
	}
	if got := MergeFiles([]string{"b", "a"}, []string{"c", "a"}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatal(got)
	}
}

func TestConflictsAndClashes(t *testing.T) {
	f := func(s string) []byte { return []byte(s) }
	base := Tree{"orders/spec.md": f(specOrders("proposed")), "billing/spec.md": f("b0")}
	theirsOrders := strings.Replace(specOrders("proposed"), "SHALL create an order.", "SHALL create an order fast.", 1)
	oursOrders := specOrders("implemented")
	projects := Tree{"orders/spec.md": f(theirsOrders), "billing/spec.md": f("b0")}
	cp := Tree{"orders/spec.md": f(oursOrders), "billing/spec.md": f("b1")}
	if c := Conflicts(projects, nil, nil, cp); c != nil {
		t.Fatalf("no base: %v", c)
	}
	c := Conflicts(projects, base, nil, cp)
	if !reflect.DeepEqual(c, []string{"orders/spec.md"}) {
		t.Fatalf("base only: %v", c)
	}
	// projects/ as the ticket's own last promotion left it is no conflict
	if c := Conflicts(projects, base, projects, cp); c != nil {
		t.Fatalf("same as after: %v", c)
	}
	anc := Ancestor(base, nil, c)
	theirs := DiffTrees("s", anc, Pick(projects, c))
	ours := DiffTrees("s", anc, Pick(cp, c))
	cl := Clashes(theirs, ours)
	if len(cl) != 1 || cl[0].Capability != "orders" || cl[0].File != "orders/spec.md" || !reflect.DeepEqual(cl[0].Requirements, []string{"REQ-ORD-1"}) {
		t.Fatalf("clashes: %+v", cl)
	}
	if cl := Clashes(theirs, DiffTrees("s", anc, anc)); len(cl) != 0 {
		t.Fatalf("no change of ours: %+v", cl)
	}
	if a := Ancestor(base, projects, c); string(a["orders/spec.md"]) != theirsOrders {
		t.Fatal("ancestor is not the after snapshot")
	}
}

func specOrders(status string) string {
	return "---\ncapability: orders\nprefix: ORD\n---\n# Orders\n\n## Purpose\nOrders.\n\n## Requirements\n\n" +
		"### REQ-ORD-1 · Create\nstatus: " + status + "\nThe system SHALL create an order.\n\n" +
		"#### Scenario: create\n- WHEN a user orders\n- THEN an order exists\n"
}

func TestRebase(t *testing.T) {
	projects := Tree{"a/spec.md": []byte("a2"), "b/spec.md": []byte("b1"), "c/spec.md": []byte("c3")}
	base := Tree{"a/spec.md": []byte("a1"), "b/spec.md": []byte("b1"), "c/spec.md": []byte("c1")}
	after := Tree{"a/spec.md": []byte("a1"), "b/spec.md": []byte("b0"), "c/spec.md": []byte("c2")}

	nb, na, changed := Rebase(projects, base, after, []string{"b/spec.md", "a/spec.md", "a/spec.md", "gone/spec.md"})
	if want := []string{"a/spec.md", "b/spec.md"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed %v, want %v", changed, want)
	}
	for _, f := range []string{"a/spec.md", "b/spec.md"} {
		if !SameFile(nb, projects, f) || !SameFile(na, projects, f) {
			t.Fatalf("%s not rebased: base %q after %q", f, nb[f], na[f])
		}
	}
	if string(nb["c/spec.md"]) != "c1" || string(na["c/spec.md"]) != "c2" {
		t.Fatalf("an unnamed file moved: base %q after %q", nb["c/spec.md"], na["c/spec.md"])
	}
	if _, ok := nb["gone/spec.md"]; ok {
		t.Fatal("a file projects lacks entered the base")
	}
	if string(base["a/spec.md"]) != "a1" || string(after["b/spec.md"]) != "b0" {
		t.Fatal("Rebase changed its inputs")
	}

	// no after snapshot: only the base moves; an entry already at projects/ is no change
	nb, na, changed = Rebase(projects, base, nil, []string{"b/spec.md", "c/spec.md"})
	if na != nil {
		t.Fatalf("after %v, want nil", na)
	}
	if want := []string{"c/spec.md"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed %v, want %v", changed, want)
	}
	if string(nb["c/spec.md"]) != "c3" {
		t.Fatalf("base c %q", nb["c/spec.md"])
	}
}

func TestSameFile(t *testing.T) {
	a := Tree{"x": []byte("1"), "y": []byte("2")}
	b := Tree{"x": []byte("1"), "y": []byte("3"), "z": nil}
	if !SameFile(a, b, "x") || SameFile(a, b, "y") || SameFile(a, b, "z") || !SameFile(a, b, "w") {
		t.Fatal("SameFile")
	}
}

// The after snapshot of a promotion holds the ticket's files only: a file
// promoted earlier keeps the previous after's content even when projects/
// changed it since, and files the ticket never promoted are left out.
func TestTicketAfter(t *testing.T) {
	projects := Tree{"a/spec.md": []byte("a-theirs"), "b/spec.md": []byte("b0"), "c/spec.md": []byte("c0")}
	prev := Tree{"a/spec.md": []byte("a-ours"), "c/spec.md": []byte("c-old")}
	got := TicketAfter(prev, projects, []string{"a/spec.md"}, Tree{"b/spec.md": []byte("b-ours")})
	want := Tree{"a/spec.md": []byte("a-ours"), "b/spec.md": []byte("b-ours")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// no previous after (promoted before there were after snapshots): the
	// promoted files come from projects/
	got = TicketAfter(nil, projects, []string{"a/spec.md"}, Tree{"b/spec.md": []byte("b-ours")})
	want = Tree{"a/spec.md": []byte("a-theirs"), "b/spec.md": []byte("b-ours")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no prev: got %q, want %q", got, want)
	}
	if prev["c/spec.md"] == nil || len(prev) != 2 {
		t.Fatal("prev changed")
	}
	// a promoted file prev lacks (a promotion that stopped part way had
	// already moved it) takes projects' entry
	got = TicketAfter(prev, projects, []string{"a/spec.md", "b/spec.md"}, Tree{})
	want = Tree{"a/spec.md": []byte("a-ours"), "b/spec.md": []byte("b0")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing from prev: got %q, want %q", got, want)
	}
}

// A file the after lacks is compared with the base alone: removed from
// projects/ since the start, it is a conflict.
func TestConflictsFileMissingFromAfter(t *testing.T) {
	base := Tree{"f/spec.md": []byte("f0")}
	cp := Tree{"f/spec.md": []byte("f1")}
	if c := Conflicts(Tree{}, base, Tree{}, cp); !reflect.DeepEqual(c, []string{"f/spec.md"}) {
		t.Fatalf("removed from projects/: %v", c)
	}
	// unchanged since the base: none
	if c := Conflicts(base, base, Tree{}, cp); c != nil {
		t.Fatalf("unchanged: %v", c)
	}
}

func TestPendingRebase(t *testing.T) {
	v := t.TempDir()
	if err := PendingRebase(v, "PROJ-12", "acme-orders"); err != nil {
		t.Fatal(err)
	}
	m := RebaseMarker(v, "PROJ-12", "acme-orders")
	if err := os.MkdirAll(filepath.Dir(m), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m, []byte("a/spec.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := PendingRebase(v, "PROJ-12", "acme-orders")
	if err == nil || !strings.Contains(err.Error(), "run ct spec start PROJ-12 acme-orders again") {
		t.Fatalf("want the rerun named, got %v", err)
	}
	if !errors.Is(err, ErrRebasePending) {
		t.Fatalf("not ErrRebasePending: %v", err)
	}
	if _, _, err := VersionSides(v, "acme-orders", Version{Ticket: "[[PROJ-12]]", Slug: "acme-orders"}); !errors.Is(err, ErrRebasePending) {
		t.Fatalf("VersionSides read a half-rebased snapshot: %v", err)
	}
	// another ticket's version is not blocked
	if _, _, err := VersionSides(v, "acme-orders", Version{Ticket: "[[PROJ-13]]", Slug: "acme-orders"}); err != nil {
		t.Fatalf("other ticket: %v", err)
	}
	if got := RefreshPending("PROJ-12", "acme-orders"); got != "refresh pending: run ct spec start PROJ-12 acme-orders" {
		t.Fatalf("note %q", got)
	}
}

func TestRebaseMarkerOutsideSnapshot(t *testing.T) {
	v := t.TempDir()
	m := RebaseMarker(v, "PROJ-12", "acme-orders")
	if filepath.Dir(m) != filepath.Dir(BaseDir(v, "PROJ-12", "acme-orders")) || filepath.Base(m) != ".acme-orders.rebase" {
		t.Fatalf("marker %s", m)
	}
}

func TestKeptList(t *testing.T) {
	v := t.TempDir()
	p := KeptList(v, "T-1", "acme-app")
	if dir := filepath.Dir(p); dir != filepath.Dir(AfterDir(v, "T-1", "acme-app")) {
		t.Fatalf("kept list %s not beside the after snapshot", p)
	}
	got, err := ReadKept(p)
	if err != nil || got != nil {
		t.Fatalf("missing list: %v %v", got, err)
	}
	if err := WriteKept(p, []string{"b/spec.md", "a/spec.md", "b/spec.md"}); err != nil {
		t.Fatal(err)
	}
	got, err = ReadKept(p)
	if err != nil || strings.Join(got, ",") != "a/spec.md,b/spec.md" {
		t.Fatalf("read back: %v %v", got, err)
	}
	after, err := ReadSnapshot(AfterDir(v, "T-1", "acme-app"))
	if err != nil || after != nil {
		t.Fatalf("the list made an after snapshot: %v %v", after, err)
	}
}

func TestUnfilled(t *testing.T) {
	projects := Tree{"a/spec.md": []byte("a"), "b/spec.md": []byte("b"), "c/spec.md": []byte("c")}
	prev := Tree{"a/spec.md": []byte("a1")}
	now := Tree{"c/spec.md": []byte("c1")}
	got := Unfilled(prev, projects, []string{"a/spec.md", "b/spec.md", "c/spec.md", "d/spec.md"}, now)
	if strings.Join(got, ",") != "b/spec.md" {
		t.Fatalf("unfilled: %v", got)
	}
	if got := Unfilled(nil, projects, []string{"b/spec.md"}, nil); got != nil {
		t.Fatalf("no prev (legacy) fills nothing it should report: %v", got)
	}
}

func TestPromotedSince(t *testing.T) {
	vs := []VersionFile{
		{Path: "n3", Version: Version{Ticket: "[[T-3]]", Promoted: "2026-01-03T00:00:00", Files: []string{"b/spec.md"}}},
		{Path: "n2", Version: Version{Ticket: "[[T-2]]", Promoted: "2026-01-02T00:00:00", Files: []string{"a/spec.md"}}},
		{Path: "n1", Version: Version{Ticket: "[[T-1]]", Promoted: "2026-01-01T00:00:00", Files: []string{"a/spec.md"}}},
	}
	if o, ok := PromotedSince(vs, "T-1", "a/spec.md", "2026-01-01T00:00:00"); !ok || o.Path != "n2" {
		t.Fatalf("want T-2's note: %+v %v", o, ok)
	}
	if _, ok := PromotedSince(vs, "T-1", "a/spec.md", "2026-01-02T00:00:00"); ok {
		t.Fatal("a promotion not after since counts")
	}
	if _, ok := PromotedSince(vs, "T-2", "a/spec.md", "2026-01-01T00:00:00"); ok {
		t.Fatal("the ticket's own note counts")
	}
}

// ChangedBy names the tickets whose after copy of the file is what
// projects/ holds now, newest first, whatever their promoted times.
func TestChangedBy(t *testing.T) {
	v := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"T-1", "T-2", "T-3", "T-4"} {
		put(filepath.Join(note.TicketDir(v, id), id+".md"), "# "+id+"\n")
	}
	after := func(id, f, s string) { put(filepath.Join(AfterDir(v, id, "app"), filepath.FromSlash(f)), s) }
	after("T-1", "b/spec.md", "b1")
	after("T-2", "b/spec.md", "b1")
	after("T-3", "b/spec.md", "b3")
	after("T-4", "b/spec.md", "b1")
	vs := []VersionFile{
		{Path: "n4", Version: Version{Ticket: "[[T-4]]", Promoted: "2026-01-04T00:00:00", Files: []string{"a/spec.md"}}},
		{Path: "n3", Version: Version{Ticket: "[[T-3]]", Promoted: "2026-01-03T00:00:00", Files: []string{"b/spec.md"}}},
		{Path: "n2", Version: Version{Ticket: "[[T-2]]", Promoted: "2026-01-02T00:00:00", Files: []string{"b/spec.md"}}},
		{Path: "n1", Version: Version{Ticket: "[[T-1]]", Promoted: "2026-01-01T00:00:00", Files: []string{"b/spec.md"}}},
	}
	paths := func(id string, cur []byte, ok bool) string {
		t.Helper()
		got, err := ChangedBy(v, "app", vs, id, "b/spec.md", cur, ok)
		if err != nil {
			t.Fatal(err)
		}
		var ps []string
		for _, vf := range got {
			ps = append(ps, vf.Path)
		}
		return strings.Join(ps, ",")
	}
	// T-4's after copy matches but its note doesn't list b; T-3's differs
	if got := paths("T-9", []byte("b1"), true); got != "n2,n1" {
		t.Fatalf("matches: %q", got)
	}
	if got := paths("T-2", []byte("b1"), true); got != "n1" {
		t.Fatalf("own note counts: %q", got)
	}
	if got := paths("T-9", []byte("b-hand"), true); got != "" {
		t.Fatalf("a hand edit names %q", got)
	}
	if got := paths("T-9", nil, false); got != "" {
		t.Fatalf("a removed file names %q", got)
	}
	if err := os.Remove(filepath.Join(AfterDir(v, "T-1", "app"), "b", "spec.md")); err != nil {
		t.Fatal(err)
	}
	if got := paths("T-9", nil, false); got != "n1" {
		t.Fatalf("a removal names %q", got)
	}
	// a ticket with no after snapshot of the repo at all is no remover
	if err := os.RemoveAll(AfterDir(v, "T-1", "app")); err != nil {
		t.Fatal(err)
	}
	if got := paths("T-9", nil, false); got != "" {
		t.Fatalf("a ticket without specs-after names %q", got)
	}
}
