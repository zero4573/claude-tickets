package ticketmigrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const statusTpl = "---\ntitle: TICKET-ID status\ncreated: {{date}}\n---\n# TICKET-ID status\n[[TICKET-ID]]\n"

func put(t *testing.T, v, rel, data string) {
	t.Helper()
	p := filepath.Join(v, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, v, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func note(status string) string { return "---\nstatus: " + status + "\n---\n# n\n" }

func vault(t *testing.T) string {
	v := t.TempDir()
	put(t, v, "tickets/A-1/A-1.md", note("active"))
	put(t, v, "tickets/A-1/board.md", v1Board)
	put(t, v, "tickets/A-1/backlog.md", "## Open\n- B-01 · x\n\n## Closed\n")
	put(t, v, "tickets/A-1/reviews/wave-1-x.md", v1Review)
	put(t, v, "tickets/A-2/A-2.md", note("closed"))
	put(t, v, "tickets/A-2/board.md", v1Board)
	put(t, v, "tickets/A-3/A-3.md", note("done"))
	put(t, v, "tickets/A-3/board.md", v1Board)
	put(t, v, "tickets/A-4/A-4.md", "# no frontmatter\n")
	put(t, v, "tickets/A-5/A-5.md", note("active"))
	put(t, v, "tickets/A-5/A-5-status.md", "mine")
	put(t, v, "tickets/A-5/board.md", ConvertBoard([]byte(v1Board)).str())
	put(t, v, "tickets/.hidden/.hidden.md", note("active"))
	put(t, v, "tickets/A-6/other.md", "no note")
	put(t, v, "tickets/loose.md", "x")
	return v
}

func (c Converted) str() string { return string(c.Data) }

func TestMigrate(t *testing.T) {
	v := vault(t)
	dry, err := Migrate(v, []byte(statusTpl), "2026-10-25", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Migrated != 3 || dry.Current != 1 || dry.Closed != 1 {
		t.Errorf("dry counts %+v", dry)
	}
	if get(t, v, "tickets/A-1/board.md") != v1Board {
		t.Error("dry run wrote")
	}
	if _, err := os.Stat(filepath.Join(v, "tickets/A-1/A-1-status.md")); err == nil {
		t.Error("dry run wrote the status file")
	}
	res, err := Migrate(v, []byte(statusTpl), "2026-10-25", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated != 3 || res.Current != 1 || res.Closed != 1 || len(res.Tickets) != 3 {
		t.Errorf("counts %+v", res)
	}
	a1 := res.Tickets[0]
	if a1.ID != "A-1" || !a1.StatusAdded {
		t.Errorf("%+v", a1)
	}
	var rels []string
	for _, f := range a1.Files {
		rels = append(rels, f.Rel)
	}
	if strings.Join(rels, ",") != "tickets/A-1/board.md,tickets/A-1/backlog.md,tickets/A-1/reviews/wave-1-x.md" {
		t.Errorf("files %v", rels)
	}
	if got := get(t, v, "tickets/A-1/A-1-status.md"); got != "---\ntitle: A-1 status\ncreated: 2026-10-25\n---\n# A-1 status\n[[A-1]]\n" {
		t.Errorf("status file %q", got)
	}
	if !strings.Contains(get(t, v, "tickets/A-1/backlog.md"), "- [ ] B-01 · x") {
		t.Error("backlog")
	}
	if get(t, v, "tickets/A-2/board.md") != v1Board {
		t.Error("closed ticket converted")
	}
	if _, err := os.Stat(filepath.Join(v, "tickets/A-2/A-2-status.md")); err == nil {
		t.Error("closed ticket got a status file")
	}
	if get(t, v, "tickets/A-5/A-5-status.md") != "mine" {
		t.Error("an existing status file was overwritten")
	}
	if !strings.Contains(get(t, v, "tickets/A-3/board.md"), "- [x] 01 · merged abc123") {
		t.Error("a done ticket wasn't migrated")
	}
	if _, err := os.Stat(filepath.Join(v, "tickets/A-4/A-4-status.md")); err != nil {
		t.Error("a note without frontmatter wasn't migrated")
	}
	if len(res.Warnings) != 2 { // the unknown status "weird" in the boards of A-1 and A-3
		t.Errorf("warnings: %v", res.Warnings)
	}
	// second run: nothing
	again, err := Migrate(v, []byte(statusTpl), "2026-10-26", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Migrated != 0 || again.Current != 4 || again.Closed != 1 || len(again.Tickets) != 0 {
		t.Errorf("second run %+v", again)
	}
}

func TestMigrateProblemsLeaveFileWhole(t *testing.T) {
	v := t.TempDir()
	bad := strings.Replace(v1Board, "| 03 | plain text | acme-ui | 2 | 01, 02 | planned | |", "| 03 | x |", 1)
	put(t, v, "tickets/A-1/A-1.md", note("active"))
	put(t, v, "tickets/A-1/board.md", bad)
	put(t, v, "tickets/A-1/reviews/r.md", "## Dispositions\n- W1-a-M01 · gibberish\n")
	res, err := Migrate(v, []byte(statusTpl), "d", false)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, v, "tickets/A-1/board.md") != bad || get(t, v, "tickets/A-1/reviews/r.md") != "## Dispositions\n- W1-a-M01 · gibberish\n" {
		t.Error("a file with problems was changed")
	}
	want := []string{
		"tickets/A-1/board.md:15: not converted (a row of 2 columns, want 7); convert it by hand",
		"tickets/A-1/reviews/r.md:2: not converted (no known form); convert it by hand",
	}
	w := 0
	for _, x := range res.Warnings {
		for _, y := range want {
			if x == y {
				w++
			}
		}
	}
	if w != 2 {
		t.Errorf("warnings %v", res.Warnings)
	}
	if res.Migrated != 1 || !res.Tickets[0].StatusAdded || len(res.Tickets[0].Files) != 0 {
		t.Errorf("%+v", res)
	}
}

func TestMigrateSymlinkAndChangedMidRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	v := t.TempDir()
	out := t.TempDir()
	put(t, v, "tickets/A-1/A-1.md", note("active"))
	put(t, out, "real-board.md", v1Board)
	if err := os.Symlink(filepath.Join(out, "real-board.md"), filepath.Join(v, "tickets/A-1/board.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(v, []byte(statusTpl), "d", false); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(filepath.Join(v, "tickets/A-1/board.md")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "real-board.md")); !strings.Contains(string(b), "- [x] 01 · merged") {
		t.Error("the target wasn't converted")
	}

	// changed between the read and the write: skipped with a warning
	put(t, v, "tickets/A-2/A-2.md", note("active"))
	put(t, v, "tickets/A-2/board.md", v1Board)
	beforeWrite = func(abs string) {
		if strings.Contains(abs, "A-2") {
			put(t, v, "tickets/A-2/board.md", v1Board+"edited by someone\n")
		}
	}
	defer func() { beforeWrite = nil }()
	res, err := Migrate(v, []byte(statusTpl), "d", false)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, v, "tickets/A-2/board.md") != v1Board+"edited by someone\n" {
		t.Error("a file changed mid-run was overwritten")
	}
	found := false
	for _, w := range res.Warnings {
		if w == "tickets/A-2/board.md: changed during the update; run it again" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings %v", res.Warnings)
	}
	// the next run converts it
	beforeWrite = nil
	if _, err := Migrate(v, []byte(statusTpl), "d", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get(t, v, "tickets/A-2/board.md"), "- [x] 01 · merged") {
		t.Error("second run didn't convert")
	}
}

func TestMigrateNoTicketsFolder(t *testing.T) {
	res, err := Migrate(t.TempDir(), []byte(statusTpl), "d", false)
	if err != nil || res.Migrated+res.Current+res.Closed != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

// W6-claude-tickets-S01: a failed status-file write is a warning; the other
// files and tickets still migrate.
func TestMigrateStatusWriteFailureIsAWarning(t *testing.T) {
	v := vault(t)
	orig := writeStatus
	t.Cleanup(func() { writeStatus = orig })
	writeStatus = func(p string, data []byte, perm os.FileMode) error {
		if strings.Contains(p, "A-1") {
			return os.ErrPermission
		}
		return orig(p, data, perm)
	}
	res, err := Migrate(v, []byte(statusTpl), "2026-10-25", false)
	if err != nil {
		t.Fatal(err)
	}
	warned := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "A-1-status.md") && strings.Contains(w, "not written") && strings.Contains(w, "run it again to add it") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("no warning: %v", res.Warnings)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets/A-1/A-1-status.md")); err == nil {
		t.Error("status file written")
	}
	if !strings.Contains(get(t, v, "tickets/A-1/board.md"), "- [x] 01") {
		t.Error("A-1's board was not converted")
	}
	for _, tc := range res.Tickets {
		if tc.ID == "A-1" && tc.StatusAdded {
			t.Error("StatusAdded set for a failed write")
		}
	}
}
