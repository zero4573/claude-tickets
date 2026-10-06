package clean

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ticket writes tickets/<id>/<id>.md with these frontmatter lines.
func ticket(t *testing.T, v, id string, fields ...string) {
	put(t, v, "tickets/"+id+"/"+id+".md", "---\n"+strings.Join(fields, "\n")+"\n---\n# "+id+"\n")
}

const jira = "source: jira\nsource-url: https://acme.atlassian.net/browse/"

func setup(t *testing.T) Options {
	v, w := t.TempDir(), t.TempDir()
	put(t, v, ".obsidian/app.json", "{}")
	return Options{Vault: v, WorkRoot: w, Now: time.Date(2026, 10, 6, 12, 0, 0, 0, time.Local),
		Running: func(string) bool { return false }}
}

func plan(o Options) map[string]Item {
	out := map[string]Item{}
	for _, it := range Plan(o) {
		out[it.ID] = it
	}
	return out
}

func TestPlanStatuses(t *testing.T) {
	o := setup(t)
	for _, st := range []string{"new", "triage", "in-progress", "blocked", "review", ""} {
		ticket(t, o.Vault, "MAN-"+st+"X", "status: "+st, "source: manual")
	}
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	ticket(t, o.Vault, "MAN-2", "status: done", "source: manual")
	ticket(t, o.Vault, "MAN-3", "status: closed", "source: manual", "ignore: true")
	ticket(t, o.Vault, "PROJ-1", "status: closed", jira+"PROJ-1")
	ticket(t, o.Vault, "PROJ-2", "status: done", jira+"PROJ-2")
	ticket(t, o.Vault, "PROJ-3", "status: closed", "source: jira", "source-url:")
	ticket(t, o.Vault, "PROJ-4", "status: closed", "source: jira", "blocks: [\"[[MAN-newX]]\"]", "source-url: https://acme.atlassian.net/browse/PROJ-4")

	got := plan(o)
	for id, want := range map[string]Action{"MAN-1": Archive, "MAN-3": Archive, "PROJ-1": Trash, "PROJ-3": Archive, "PROJ-4": Trash} {
		if got[id].Action != want {
			t.Errorf("%s: %+v, want action %d", id, got[id], want)
		}
	}
	if len(got) != 5 {
		t.Errorf("open or done tickets planned: %v", got)
	}

	o.IncludeDone = true
	got = plan(o)
	if got["MAN-2"].Action != Archive || got["PROJ-2"].Action != Skip || !strings.Contains(got["PROJ-2"].Reason, "done synced ticket") {
		t.Errorf("--include-done: %+v %+v", got["MAN-2"], got["PROJ-2"])
	}

	o.IncludeDone, o.KeepManual, o.Purge = false, true, true
	got = plan(o)
	if got["MAN-1"].Action != Keep || got["PROJ-3"].Action != Keep || got["PROJ-1"].Action != Purge {
		t.Errorf("--keep-manual --purge: %+v", got)
	}
}

func TestPlanExplicit(t *testing.T) {
	o := setup(t)
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	ticket(t, o.Vault, "MAN-2", "status: in-progress", "source: manual")
	ticket(t, o.Vault, "MAN-3", "status: done", "source: manual")
	o.IDs = []string{"MAN-2", "MAN-9", "MAN-1", "nope", "MAN-3"}
	items := Plan(o)
	if len(items) != 5 {
		t.Fatalf("items = %+v", items)
	}
	want := []string{"not finished (status in-progress)", "not a ticket in the vault", "", "not a ticket in the vault", "done, not closed"}
	for i, it := range items {
		if !it.Explicit || !strings.HasPrefix(it.Reason, want[i]) {
			t.Errorf("%s: %+v, want reason %q", it.ID, it, want[i])
		}
	}
}

func TestPlanDays(t *testing.T) {
	o := setup(t)
	ticket(t, o.Vault, "MAN-1", "status: closed", "updated: 2026-10-01")
	ticket(t, o.Vault, "MAN-2", "status: closed", "updated: 2026-09-01")
	o.Days = 30
	got := plan(o)
	if got["MAN-1"].Action != Skip || got["MAN-1"].Reason != "updated 5 day(s) ago (--days 30)" || got["MAN-2"].Action != Archive {
		t.Errorf("--days: %+v", got)
	}
}

func TestPlanLeads(t *testing.T) {
	o := setup(t)
	lead := []string{"status: closed", "source: manual", "ticket-type: epic", `covers: ["[[MAN-2]]", "[[MAN-3]]"]`}
	ticket(t, o.Vault, "MAN-1", lead...)
	ticket(t, o.Vault, "MAN-2", "status: closed", "source: manual", `covered-by: "[[MAN-1]]"`)
	ticket(t, o.Vault, "MAN-3", "status: review", "source: manual", `covered-by: "[[MAN-1]]"`)
	got := plan(o)
	if got["MAN-1"].Reason != "waiting on covered tickets: MAN-3" || got["MAN-2"].Action != Archive {
		t.Errorf("lead with an open covered ticket: %+v", got)
	}
	ticket(t, o.Vault, "MAN-3", "status: closed", "source: manual", `covered-by: "[[MAN-1]]"`)
	got = plan(o)
	if got["MAN-1"].Action != Archive || got["MAN-3"].Action != Archive {
		t.Errorf("lead cleaned with its covered tickets: %+v", got)
	}
	if order := Order(Plan(o)); order[0].ID != "MAN-2" || order[2].ID != "MAN-1" {
		t.Errorf("covered tickets go first: %v", order)
	}
	// Named alone, the lead waits for its covered tickets
	o.IDs = []string{"MAN-1"}
	if got := plan(o); got["MAN-1"].Action != Skip {
		t.Errorf("lead named alone: %+v", got)
	}
	o.IDs = nil
	ticket(t, o.Vault, "MAN-1", "status: review", "source: manual", "ticket-type: epic", `covers: ["[[MAN-2]]", "[[MAN-3]]"]`)
	if got := plan(o); got["MAN-2"].Reason != "lead MAN-1 still open" {
		t.Errorf("covered with an open lead: %+v", got)
	}
	// A covered ticket whose lead is gone qualifies
	os.RemoveAll(filepath.Join(o.Vault, "tickets", "MAN-1"))
	if got := plan(o); got["MAN-2"].Action != Archive {
		t.Errorf("lead gone: %+v", got)
	}
}

func TestPlanSaved(t *testing.T) {
	o := setup(t)
	ticket(t, o.Vault, "PROJ-1", "status: closed", jira+"PROJ-1")
	put(t, o.Vault, "tickets/PROJ-1/design.md", "x")
	ticket(t, o.Vault, "PROJ-2", "status: closed", jira+"PROJ-2", "saved: 2026-10-01T10:00:00")
	put(t, o.Vault, "tickets/PROJ-2/design.md", "x")
	got := plan(o)
	if got["PROJ-1"].Action != Skip || !strings.HasPrefix(got["PROJ-1"].Reason, "needs /tickets:save") ||
		!strings.Contains(got["PROJ-1"].Task, "[[PROJ-1]] (closed) wasn't saved") || got["PROJ-2"].Action != Trash {
		t.Errorf("unsaved: %+v", got)
	}
	// --save needs a workspace to save in, with target versions
	o.Save = true
	if got := plan(o); got["PROJ-1"].Action != Skip {
		t.Errorf("--save without a workspace: %+v", got["PROJ-1"])
	}
	put(t, o.WorkRoot, "PROJ-1/workspace.json", `{"id": "PROJ-1", "repos": []}`)
	if got := plan(o); !got["PROJ-1"].Save || got["PROJ-1"].SaveIn != "PROJ-1" || got["PROJ-1"].Action != Trash {
		t.Errorf("--save: %+v", got["PROJ-1"])
	}
	os.MkdirAll(filepath.Join(o.WorkRoot, "PROJ-1", "repo"), 0o755)
	put(t, o.WorkRoot, "PROJ-1/workspace.json", `{"id": "PROJ-1", "repos": [{"slug": "repo", "path": "`+filepath.Join(o.WorkRoot, "PROJ-1", "repo")+`", "targetVersion": null}]}`)
	if got := plan(o); got["PROJ-1"].Action != Skip || !strings.Contains(got["PROJ-1"].Reason, "repo has no target version") {
		t.Errorf("--save, null target version: %+v", got["PROJ-1"])
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=x", "GIT_AUTHOR_EMAIL=x@example.com",
		"GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL=x@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestPlanSafety(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	o := setup(t)
	ticket(t, o.Vault, "PROJ-1", "status: closed", jira+"PROJ-1", "saved: 2026-10-01T10:00:00")
	root := t.TempDir()
	remote, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	wt := filepath.Join(o.WorkRoot, "PROJ-1", "repo")
	git(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	git(t, root, "clone", "--quiet", remote, clone)
	git(t, clone, "commit", "--quiet", "--allow-empty", "-m", "base")
	git(t, clone, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "worktree", "add", "--quiet", "-b", "feature/PROJ-1", wt)
	put(t, o.WorkRoot, "PROJ-1/workspace.json", `{"id": "PROJ-1", "repos": [{"slug": "repo", "path": "`+wt+`", "base": "main", "targetVersion": "1.0"}]}`)

	if got := plan(o); got["PROJ-1"].Action != Trash {
		t.Fatalf("clean and pushed: %+v", got["PROJ-1"])
	}
	put(t, wt, "work.txt", "work")
	git(t, wt, "add", "work.txt")
	git(t, wt, "commit", "--quiet", "-m", "work")
	if got := plan(o); got["PROJ-1"].Reason != "commits on no remote in repo" || got["PROJ-1"].Task == "" {
		t.Errorf("unpushed worktree: %+v", got["PROJ-1"])
	}
	put(t, wt, "new.txt", "x")
	if got := plan(o); got["PROJ-1"].Reason != "uncommitted changes in repo" {
		t.Errorf("dirty: %+v", got["PROJ-1"])
	}
	o.Running = func(string) bool { return true }
	if got := plan(o); got["PROJ-1"].Reason != "session running (PROJ-1)" || got["PROJ-1"].Task != "" {
		t.Errorf("running: %+v", got["PROJ-1"])
	}
}

func TestRun(t *testing.T) {
	o := setup(t)
	v := o.Vault
	ticket(t, v, "PROJ-12", "status: closed", jira+"PROJ-12", "saved: 2026-10-01T10:00:00")
	put(t, v, "tickets/PROJ-12/logs/x.md", "log")
	put(t, v, "tickets/PROJ-12/design.md", "design")
	ticket(t, v, "PROJ-13", "status: in-progress", "source: jira", `related: ["[[PROJ-12]]"]`)
	put(t, v, "tickets/PROJ-13/PROJ-13.md", "---\nstatus: in-progress\nsource: jira\nrelated: [\"[[PROJ-12]]\"]\n---\n<!-- source:start -->\n[[PROJ-12]]\n<!-- source:end -->\nwork on [[PROJ-12|it]]\n")
	ticket(t, v, "MAN-4", "status: closed", "source: manual")
	ticket(t, v, "MAN-5", "status: closed", "source: manual")
	put(t, v, "tickets/MAN-5/requirements.md", "x")
	put(t, v, "projects/p/p.md", "---\nrelated: [\"[[PROJ-12]]\"]\n---\n[[PROJ-12]] [[MAN-4]] ![[tickets/PROJ-12/logs/x]]\n")
	put(t, o.WorkRoot, "MAN-4/workspace.json", `{"id": "MAN-4", "repos": []}`)

	items := Plan(o)
	var desc []string
	for _, it := range items {
		desc = append(desc, Describe(o, items, it))
	}
	want := []string{
		"MAN-4 (closed): remove the workspace; archive to archive/tickets/MAN-4",
		"MAN-5 (closed): skip: needs /tickets:save (worked (requirements.md), not saved)",
		"PROJ-12 (closed): move to .trash/PROJ-12; rewrite 3 link(s) in 2 note(s) to https://acme.atlassian.net/browse/PROJ-12; keep 1 file(s) in archive/tickets/PROJ-12",
	}
	if strings.Join(desc, "\n") != strings.Join(want, "\n") {
		t.Errorf("plan:\n%s\nwant\n%s", strings.Join(desc, "\n"), strings.Join(want, "\n"))
	}

	var out bytes.Buffer
	var rmd []string
	res := Run(o, items, Env{Out: &out, RemoveWorkspace: func(id string) error {
		rmd = append(rmd, id)
		return os.RemoveAll(filepath.Join(o.WorkRoot, id))
	}})
	if res.Failed || res.Cleaned != 2 || strings.Join(rmd, ",") != "MAN-4" || res.FollowUp == "" {
		t.Errorf("run: %+v %v\n%s", res, rmd, out.String())
	}
	for _, s := range []string{
		"PROJ-12: moved to .trash/PROJ-12; rewrote 3 link(s) in 2 note(s); kept 1 file(s) in archive/tickets/PROJ-12",
		"MAN-4: removed its workspace; archived 1 file(s) to archive/tickets/MAN-4",
	} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("missing %q in\n%s", s, out.String())
		}
	}
	url := "https://acme.atlassian.net/browse/PROJ-12"
	read := func(rel string) string { data, _ := os.ReadFile(filepath.Join(v, rel)); return string(data) }
	if got := read("projects/p/p.md"); got != "---\nrelated: [\"[PROJ-12]("+url+")\"]\n---\n[PROJ-12]("+url+") [[MAN-4]] ![[archive/tickets/PROJ-12/logs/x]]\n" {
		t.Errorf("p.md: %q", got)
	}
	if got := read("tickets/PROJ-13/PROJ-13.md"); !strings.Contains(got, "related: [\"[[PROJ-12]]\"]") ||
		!strings.Contains(got, "start -->\n[[PROJ-12]]\n") || !strings.Contains(got, "work on [it]("+url+")") {
		t.Errorf("synced ticket: %q", got)
	}
	for _, p := range []string{".trash/PROJ-12/PROJ-12.md", ".trash/PROJ-12/design.md", "archive/tickets/PROJ-12/logs/x.md", "archive/tickets/MAN-4/MAN-4.md", "tickets/MAN-5/MAN-5.md"} {
		if _, err := os.Stat(filepath.Join(v, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{"tickets/PROJ-12", "tickets/MAN-4", ".vault.lock.d"} {
		if _, err := os.Stat(filepath.Join(v, p)); err == nil {
			t.Errorf("%s still there", p)
		}
	}
	if !strings.Contains(read(rel(v, res.FollowUp)), "[[MAN-5]] (closed) wasn't saved") {
		t.Errorf("follow-up: %s", read(rel(v, res.FollowUp)))
	}
	// Idempotent
	if items := Plan(o); len(items) != 1 || items[0].ID != "MAN-5" {
		t.Errorf("second plan: %+v", items)
	}
}

func TestRunSave(t *testing.T) {
	o := setup(t)
	o.Save = true
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	put(t, o.Vault, "tickets/MAN-1/design.md", "x")
	ticket(t, o.Vault, "MAN-2", "status: closed", "source: manual")
	put(t, o.Vault, "tickets/MAN-2/design.md", "x")
	put(t, o.WorkRoot, "MAN-1/workspace.json", `{"id": "MAN-1", "repos": []}`)
	put(t, o.WorkRoot, "MAN-2/workspace.json", `{"id": "MAN-2", "repos": []}`)
	put(t, o.WorkRoot, "MAN-2/notes.txt", "x")
	items := Plan(o)
	var out bytes.Buffer
	res := Run(o, items, Env{Out: &out,
		Save: func(id string) (string, error) {
			if id == "MAN-1" {
				f := filepath.Join(o.Vault, "tickets/MAN-1/MAN-1.md")
				data, _ := os.ReadFile(f)
				os.WriteFile(f, []byte(strings.Replace(string(data), "---\n", "---\nsaved: "+time.Now().Format("2006-01-02T15:04:05")+"\n", 1)), 0o644)
				return "log", nil
			}
			return "save.log", os.ErrInvalid
		},
		RemoveWorkspace: func(id string) error { return os.RemoveAll(filepath.Join(o.WorkRoot, id)) },
	})
	if res.Cleaned != 1 || !strings.Contains(out.String(), "MAN-2: skipped: /tickets:save failed (invalid argument); see save.log") {
		t.Errorf("%+v\n%s", res, out.String())
	}
	if _, err := os.Stat(filepath.Join(o.Vault, "archive/tickets/MAN-1/MAN-1.md")); err != nil {
		t.Error("saved ticket not archived")
	}
}

// Regression: --keep-manual leaves a ticket as it is, so --save never saves
// it; and any planned save counts as an action (shown, confirmed).
func TestPlanKeepIsNotSaved(t *testing.T) {
	o := setup(t)
	o.Save, o.KeepManual = true, true
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	put(t, o.Vault, "tickets/MAN-1/design.md", "x")
	put(t, o.WorkRoot, "MAN-1/workspace.json", `{"id": "MAN-1", "repos": []}`)
	it := plan(o)["MAN-1"]
	if it.Action != Keep || it.Save || it.Acts() {
		t.Errorf("kept ticket: %+v", it)
	}
	if !(Item{Action: Skip, Save: true}).Acts() || !(Item{Action: Keep, Save: true}).Acts() {
		t.Error("a save doesn't count as an action")
	}
}

// Regression: a folder at <workRoot>/<ID> without workspace.json isn't a
// workspace: the plan says it stays, and the run leaves it.
func TestRunLeavesNonWorkspaceFolder(t *testing.T) {
	o := setup(t)
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	put(t, o.WorkRoot, "MAN-1/precious.txt", "x")
	items := Plan(o)
	ws := filepath.Join(o.WorkRoot, "MAN-1")
	if d := Describe(o, items, items[0]); d != "MAN-1 (closed): leave "+ws+" (no workspace.json); archive to archive/tickets/MAN-1" {
		t.Errorf("plan: %s", d)
	}
	var out bytes.Buffer
	called := false
	Run(o, items, Env{Out: &out, RemoveWorkspace: func(string) error { called = true; return nil }})
	if called || !strings.Contains(out.String(), "MAN-1: left "+ws+" in place (no workspace.json); archived 1 file(s)") {
		t.Errorf("run (wsRm called: %t):\n%s", called, out.String())
	}
	if _, err := os.Stat(filepath.Join(ws, "precious.txt")); err != nil {
		t.Error("the folder's file is gone")
	}

	// A workspace whose removal leaves other files says so
	ticket(t, o.Vault, "MAN-2", "status: closed", "source: manual")
	put(t, o.WorkRoot, "MAN-2/workspace.json", `{"id": "MAN-2", "repos": []}`)
	out.Reset()
	Run(o, Plan(o), Env{Out: &out, RemoveWorkspace: func(id string) error {
		return os.Remove(filepath.Join(o.WorkRoot, id, "workspace.json"))
	}})
	if !strings.Contains(out.String(), "MAN-2: removed its workspace, but left "+filepath.Join(o.WorkRoot, "MAN-2")+" in place: it still has other files") {
		t.Errorf("run:\n%s", out.String())
	}
}
