package note

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

const summary = "---\ncreated: *2026-10-01T10:00:00*\n\nFixed it.\n"

func TestSaveStateOf(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  SaveState
	}{
		{"marker", map[string]string{"PROJ-1.md": "---\nstatus: closed\nsaved: 2026-10-01T10:00:00\n---\n# x\n", "design.md": "x"}, Saved},
		{"summary block at the end", map[string]string{"PROJ-1.md": "---\nstatus: closed\n---\n# x\n\n" + summary, "design.md": "x"}, Saved},
		{"summary block in the middle", map[string]string{"PROJ-1.md": "---\nstatus: closed\n---\n# x\n\n" + summary + "\n## Later\n", "design.md": "x"}, Saved},
		{"summary block, CRLF", map[string]string{"PROJ-1.md": "---\r\nstatus: closed\r\n---\r\n# x\r\n\r\n---\r\ncreated: *2026-10-01T10:00:00*\r\n", "design.md": "x"}, Saved},
		{"frontmatter created isn't a summary", map[string]string{"PROJ-1.md": "---\ncreated: *2026-10-01T10:00:00*\n---\n# x\n", "design.md": "x"}, Unsaved},
		{"pending draft overrides the marker", map[string]string{"PROJ-1.md": "---\nsaved: 2026-10-01T10:00:00\n---\n", "kb-drafts/flow.md": "---\nstatus: draft\n---\n"}, Unsaved},
		{"merged draft is fine", map[string]string{"PROJ-1.md": "---\nsaved: 2026-10-01T10:00:00\n---\n", "kb-drafts/flow.md": "---\nstatus: merged\n---\n"}, Saved},
		{"never worked", map[string]string{"PROJ-1.md": "---\nstatus: closed\n---\n# x\n"}, NothingToSave},
		{"hand-off files, no save", map[string]string{"PROJ-1.md": "---\nstatus: closed\n---\n", "requirements.md": "x"}, Unsaved},
		{"logs only", map[string]string{"PROJ-1.md": "---\nstatus: closed\n---\n", "logs/x.md": "x"}, Unsaved},
	} {
		v := t.TempDir()
		for rel, body := range tc.files {
			put(t, v, filepath.Join("tickets", "PROJ-1", rel), body)
		}
		if got, why := SaveStateOf(v, "", "PROJ-1"); got != tc.want {
			t.Errorf("%s: %s (%s), want %s", tc.name, got, why, tc.want)
		}
	}
}

func TestSaveStateOfWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	v, ws := t.TempDir(), t.TempDir()
	put(t, v, "tickets/MAN-1/MAN-1.md", "---\nstatus: closed\n---\n")
	repo := filepath.Join(ws, "repo")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=x", "GIT_AUTHOR_EMAIL=x@example.com",
			"GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL=x@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(repo, 0o755)
	git("init", "--quiet", "-b", "main")
	git("commit", "--quiet", "--allow-empty", "-m", "base")
	git("checkout", "--quiet", "-b", "feature/MAN-1")
	put(t, ws, "workspace.json", `{"id": "MAN-1", "repos": [{"slug": "repo", "path": "`+repo+`", "base": "main"}]}`)
	if got, why := SaveStateOf(v, ws, "MAN-1"); got != NothingToSave {
		t.Errorf("no commits: %s (%s)", got, why)
	}
	git("commit", "--quiet", "--allow-empty", "-m", "work")
	if got, why := SaveStateOf(v, ws, "MAN-1"); got != Unsaved || why != "commits in repo, not saved" {
		t.Errorf("a commit: %s (%s)", got, why)
	}
}

func TestTicketHelpers(t *testing.T) {
	v := t.TempDir()
	for _, d := range []string{"archive/tickets/MAN-4", "archive/tickets/PROJ-12", "archive/tickets/notes"} {
		os.MkdirAll(filepath.Join(v, d), 0o755)
	}
	if got := ArchivedIDs(v); len(got) != 2 || got[0] != "MAN-4" || got[1] != "PROJ-12" {
		t.Errorf("ArchivedIDs = %v", got)
	}
	if TicketDir(v, "MAN-4") != filepath.Join(v, "tickets", "MAN-4") || ArchiveDir(v, "MAN-4") != filepath.Join(v, "archive", "tickets", "MAN-4") {
		t.Error("TicketDir / ArchiveDir")
	}
	if !IsTicketNote(filepath.Join(v, "tickets", "MAN-4", "MAN-4.md")) || IsTicketNote(filepath.Join(v, "tickets", "MAN-4", "design.md")) ||
		IsTicketNote(filepath.Join(v, "projects", "x", "x.md")) {
		t.Error("IsTicketNote")
	}
	synced := map[string]string{"source": "jira"}
	manual := map[string]string{"source": "manual"}
	for _, tc := range []struct {
		fm     map[string]string
		field  string
		block  bool
		synced bool
	}{
		{synced, "related", false, true},
		{synced, "covered-by", false, true},
		{synced, "source-url", false, true},
		{synced, "", true, true},
		{synced, "projects", false, false},
		{synced, "", false, false},
		{manual, "related", false, false},
		{manual, "", true, false},
		{map[string]string{}, "related", false, false},
	} {
		if got := SyncOwned(tc.fm, tc.field, tc.block); got != tc.synced {
			t.Errorf("SyncOwned(%v, %q, %t) = %t", tc.fm, tc.field, tc.block, got)
		}
	}
}
