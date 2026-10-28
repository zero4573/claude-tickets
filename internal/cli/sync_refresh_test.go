package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/mcp/mcptest"
	"github.com/zero4573/claude-tickets/internal/vault"
)

// syncEnv is a throwaway HOME and vault with a tickets/.sources.json, and
// refreshIndex recording its calls.
func syncEnv(t *testing.T, sources string) (vault.Context, *[][]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CLAUDE_TICKETS_CLAUDE", "true") // the headless step runs and does nothing
	t.Setenv("CLAUDE_TICKETS_MCP_CONFIG", "")
	t.Setenv("CLAUDE_TICKETS_MCP_PREPARE", "")
	v := filepath.Join(home, "vault")
	write(t, filepath.Join(v, "tickets", ".sources.json"), sources)
	if err := os.MkdirAll(filepath.Join(v, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	tpl, err := os.ReadFile("../../assets/vault-scaffold/templates/ticket-synced.md")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(v, "templates", "ticket-synced.md"), string(tpl))
	var calls [][]string
	orig := refreshIndex
	t.Cleanup(func() { refreshIndex = orig })
	refreshIndex = func(_ string, files ...string) { calls = append(calls, files) }
	ctx := vault.Context{Vault: v}
	ctx.ProjectsRoot = filepath.Join(home, "projects")
	ctx.WorkRoot = filepath.Join(home, "work")
	return ctx, &calls
}

func fakeJira(t *testing.T, open ...string) {
	t.Helper()
	f, err := mcptest.LoadFixture("../../testdata/sync/state3.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Open = open
	s := mcptest.New(f)
	t.Cleanup(s.Close)
	cfg := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(cfg, []byte(s.Config("atlassian")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_TICKETS_MCP_CONFIG", cfg)
}

const jiraSources = `{"sources": {"jira": {"enabled": true, "mcp": "atlassian", "idPrefix": "", "query": "q"}}}`

func TestTicketSyncRefreshesWrittenNotes(t *testing.T) {
	ctx, calls := syncEnv(t, jiraSources)
	fakeJira(t, "PROJ-1")
	if err := ticketSync(ctx, "", false); err != nil {
		t.Logf("ticketSync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctx.Vault, "tickets/jira/PROJ-1/PROJ-1.md")); err != nil {
		t.Fatalf("the ticket note wasn't written: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("want one refresh, got %v", *calls)
	}
	got := (*calls)[0]
	found := false
	for _, p := range got {
		if filepath.IsAbs(p) || !strings.HasPrefix(p, "tickets/") && !strings.HasPrefix(p, "inbox/") {
			t.Fatalf("unexpected path %q in %v", p, got)
		}
		found = found || p == "tickets/jira/PROJ-1/PROJ-1.md"
	}
	if !found {
		t.Fatalf("PROJ-1 note missing from %v", got)
	}
	if want := "tickets/jira/PROJ-1/PROJ-1.md"; strings.Join(got, " ") != want {
		t.Fatalf("want exactly %q, got %v", want, got)
	}
}

func TestTicketSyncNoRefreshWhenSyncFalse(t *testing.T) {
	ctx, calls := syncEnv(t, `{"sources": {"jira": {"enabled": true, "sync": false, "mcp": "atlassian"}}}`)
	if err := ticketSync(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing ran, want no refresh: %v", *calls)
	}
}

func TestTicketSyncUnreachableRefreshesFollowupOnly(t *testing.T) {
	ctx, calls := syncEnv(t, jiraSources)
	t.Setenv("CLAUDE_TICKETS_MCP_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	_ = ticketSync(ctx, "", false)
	if len(*calls) != 1 || len((*calls)[0]) != 1 {
		t.Fatalf("want one call with the follow-up note only, got %v", *calls)
	}
	note := (*calls)[0][0]
	if !strings.HasPrefix(note, "inbox/") {
		t.Fatalf("want an inbox note, got %q", note)
	}
	if _, err := os.Stat(filepath.Join(ctx.Vault, filepath.FromSlash(note))); err != nil {
		t.Fatal(err)
	}
}
