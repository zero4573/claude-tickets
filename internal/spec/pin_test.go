package spec

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// pinRepo is a git repo for the pin tests; each commit gets the next fixed
// date, so --since=<pinned> is deterministic.
type pinRepo struct {
	t   *testing.T
	dir string
	day int
}

func newPinRepo(t *testing.T) *pinRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	r := &pinRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write("specs/orders/spec.md", "one\n")
	r.commit("base")
	return r
}

func (r *pinRepo) date() string {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local).AddDate(0, 0, r.day).Format(TimeLayout)
}

func (r *pinRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.name=x", "-c", "user.email=x@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+r.date(), "GIT_COMMITTER_DATE="+r.date(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *pinRepo) write(file, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *pinRepo) commit(msg string) string {
	r.t.Helper()
	r.day++
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// feature makes a two-commit branch off main and returns its head, with a
// version pinned to it the day after.
func (r *pinRepo) feature(name string) (string, Version) {
	r.git("checkout", "-q", "-b", name, "main")
	r.write("app/"+name+".txt", "a\n")
	r.commit("first")
	r.write("app/"+name+".txt", "a\nb\n")
	head := r.commit("second")
	r.day++
	v := Version{Commit: head, Pinned: r.date()}
	r.git("checkout", "-q", "main")
	return head, v
}

func TestPushedHead(t *testing.T) {
	r := newPinRepo(t)
	head := r.git("rev-parse", "HEAD")
	if _, ok, err := PushedHead(r.dir, "main"); ok || err != nil {
		t.Fatalf("no origin branch: ok=%v err=%v", ok, err)
	}
	r.git("update-ref", "refs/remotes/origin/main", head)
	if sha, ok, err := PushedHead(r.dir, "main"); !ok || err != nil || sha != head {
		t.Fatalf("pushed: %q %v %v", sha, ok, err)
	}
	r.write("x.txt", "x\n")
	r.commit("ahead")
	if _, ok, err := PushedHead(r.dir, "main"); ok || err != nil {
		t.Fatalf("ahead of origin: ok=%v err=%v", ok, err)
	}
	if _, _, err := PushedHead(r.dir, "nope"); err == nil {
		t.Fatal("a missing local branch must be an error")
	}
}

func TestPinAndRepin(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.Local)
	v := Version{}
	if !Pin(&v, "aaa", now) || v.Commit != "aaa" || v.Pinned != "2026-03-04T05:06:07" {
		t.Fatalf("first pin: %+v", v)
	}
	v.Merged, v.Tags = "mmm", []string{"v1"}
	if Pin(&v, "aaa", now.Add(time.Hour)) || v.Merged != "mmm" || v.Pinned != "2026-03-04T05:06:07" {
		t.Fatalf("same commit must keep the pin: %+v", v)
	}
	if !Pin(&v, "bbb", now.Add(time.Hour)) || v.Commit != "bbb" || v.Merged != "" || v.Tags != nil || v.Pinned != "2026-03-04T06:06:07" {
		t.Fatalf("a rewritten branch re-pins and drops the old resolution: %+v", v)
	}
}

func TestResolveMergeCommit(t *testing.T) {
	r := newPinRepo(t)
	head, v := r.feature("feat")
	r.write("other.txt", "o\n")
	r.commit("other")
	r.day++
	r.git("merge", "-q", "--no-ff", "-m", "merge feat", "feat")
	if m, err := ResolveMerged(r.dir, "main", v); err != nil || m != head {
		t.Fatalf("merge commit: merged = %q, %v; want the pinned commit", m, err)
	}
}

func TestResolveSquash(t *testing.T) {
	r := newPinRepo(t)
	_, v := r.feature("feat")
	r.write("other.txt", "o\n")
	r.commit("other")
	r.git("merge", "-q", "--squash", "feat")
	sq := r.commit("squash feat")
	r.write("later.txt", "l\n")
	r.commit("later")
	if m, err := ResolveMerged(r.dir, "main", v); err != nil || m != sq {
		t.Fatalf("squash: merged = %q, %v; want %s", m, err, sq)
	}

	// a commit with the same change from well before the pin (past the
	// two-day clock margin) isn't it
	r.day += 3
	v.Pinned = r.date()
	r.day++
	if m, err := ResolveMerged(r.dir, "main", v); err != nil || m != "" {
		t.Fatalf("squash older than the pin: merged = %q, %v", m, err)
	}
}

func TestResolveConflictFixedSquashNotFound(t *testing.T) {
	r := newPinRepo(t)
	_, v := r.feature("feat")
	r.git("merge", "-q", "--squash", "feat")
	r.write("app/feat.txt", "a\nb fixed\n")
	r.commit("squash feat, fixed")
	m, err := ResolveMerged(r.dir, "main", v)
	if err != nil || m != "" {
		t.Fatalf("a squash with fixes must not match: merged = %q, %v", m, err)
	}
	changed, err := Refresh(r.dir, "main", &v)
	if err != nil || changed || v.Merged != "" {
		t.Fatalf("refresh of an unmatched pin: changed=%v %+v %v", changed, v, err)
	}
}

func TestResolveUnknownAndUnpinned(t *testing.T) {
	r := newPinRepo(t)
	if m, err := ResolveMerged(r.dir, "main", Version{}); m != "" || err != nil {
		t.Fatalf("unpinned: %q %v", m, err)
	}
	if m, err := ResolveMerged(r.dir, "main", Version{Commit: strings.Repeat("ab", 20)}); m != "" || err != nil {
		t.Fatalf("a commit the clone lacks: %q %v", m, err)
	}
	head := r.git("rev-parse", "HEAD")
	if _, err := ResolveMerged(r.dir, "nope", Version{Commit: head}); err == nil {
		t.Fatal("a base that doesn't resolve must be an error")
	}
}

func TestRefreshTags(t *testing.T) {
	r := newPinRepo(t)
	head, v := r.feature("feat")
	r.git("tag", "rc-feat", head)
	r.git("merge", "-q", "--no-ff", "-m", "merge feat", "feat")
	changed, err := Refresh(r.dir, "main", &v)
	if err != nil || !changed || v.Merged != head || !reflect.DeepEqual(v.Tags, []string{"rc-feat"}) {
		t.Fatalf("first refresh: changed=%v %+v %v", changed, v, err)
	}
	r.git("tag", "v1.10.0")
	r.write("next.txt", "n\n")
	r.commit("next")
	r.git("tag", "v1.9.0")
	changed, err = Refresh(r.dir, "main", &v)
	if err != nil || !changed || !reflect.DeepEqual(v.Tags, []string{"rc-feat", "v1.9.0", "v1.10.0"}) {
		t.Fatalf("later tags, oldest version first: changed=%v %v %v", changed, v.Tags, err)
	}
	if changed, err := Refresh(r.dir, "main", &v); err != nil || changed {
		t.Fatalf("nothing new: changed=%v %v", changed, err)
	}
	u := Version{}
	if changed, err := Refresh(r.dir, "main", &u); err != nil || changed {
		t.Fatalf("unpinned: changed=%v %v", changed, err)
	}
}

func TestVersionSidesAndUnderCapability(t *testing.T) {
	vault := t.TempDir()
	id, slug := "PROJ-12", "acme-orders"
	if err := os.MkdirAll(filepath.Join(vault, "tickets", "jira", id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "tickets", "jira", id, id+".md"), []byte("# t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := Version{Slug: slug, Ticket: "[[" + id + "]]", Files: []string{"orders/spec.md"}}
	if b, a, err := VersionSides(vault, slug, v); err != nil || len(b) != 0 || len(a) != 0 {
		t.Fatalf("no snapshots: %v %v %v", b, a, err)
	}
	base := Tree{"orders/spec.md": []byte("old\n"), "billing/spec.md": []byte("b\n")}
	if err := WriteTree(BaseDir(vault, id, slug), base); err != nil {
		t.Fatal(err)
	}
	projects := Tree{"orders/spec.md": []byte("new\n"), "billing/spec.md": []byte("other ticket\n")}
	if err := WriteTree(VaultDir(vault, slug), projects); err != nil {
		t.Fatal(err)
	}
	b, a, err := VersionSides(vault, slug, v)
	if err != nil || string(b["orders/spec.md"]) != "old\n" || string(a["orders/spec.md"]) != "new\n" || string(a["billing/spec.md"]) != "b\n" {
		t.Fatalf("base only: %v %v %v", b, a, err)
	}
	after := Tree{"orders/spec.md": []byte("mine\n"), "billing/spec.md": []byte("later\n")}
	if err := WriteTree(AfterDir(vault, id, slug), after); err != nil {
		t.Fatal(err)
	}
	b, a, err = VersionSides(vault, slug, v)
	if err != nil || string(b["orders/spec.md"]) != "old\n" || string(a["orders/spec.md"]) != "mine\n" || string(a["billing/spec.md"]) != "b\n" {
		t.Fatalf("base and after: %v %v %v", b, a, err)
	}
	if got := UnderCapability(a, "orders"); len(got) != 1 || got["orders/spec.md"] == nil {
		t.Fatalf("UnderCapability = %v", got)
	}
}

func TestSinceArgWidensThePinnedTime(t *testing.T) {
	got := sinceArg("2026-10-29T12:00:00")
	want := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local).Format(time.RFC3339)
	if got != want {
		t.Errorf("sinceArg = %q, want %q", got, want)
	}
	if sinceArg("not a time") != "" {
		t.Error("an unparsable time must search the whole base")
	}
}

func TestReapplyKeepsARewrittenNote(t *testing.T) {
	r := newPinRepo(t)
	head, _ := r.feature("feature/x")
	path := filepath.Join(t.TempDir(), "note.md")
	write := func(v Version) {
		t.Helper()
		b, err := RenderVersion(v, "tickets/x", Tree{}, Diff{})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// read once, then another save promotes: new files, pin cleared
	write(Version{Slug: "s", Ticket: "[[X-1]]", Files: []string{"a/spec.md"}, Commit: "old", Pinned: r.date()})
	write(Version{Slug: "s", Ticket: "[[X-1]]", Files: []string{"a/spec.md", "b/spec.md"}})
	v, pinned, changed, err := Reapply(path, r.dir, "main", head, time.Now())
	if err != nil || !pinned || !changed {
		t.Fatalf("pinned=%v changed=%v err=%v", pinned, changed, err)
	}
	if !reflect.DeepEqual(v.Files, []string{"a/spec.md", "b/spec.md"}) || v.Commit != head {
		t.Fatalf("the fresh note must win and be pinned: %+v", v)
	}
	// without a sha nothing is pinned
	if _, pinned, _, err := Reapply(path, r.dir, "main", "", time.Now()); err != nil || pinned {
		t.Fatalf("pinned=%v err=%v", pinned, err)
	}
}
