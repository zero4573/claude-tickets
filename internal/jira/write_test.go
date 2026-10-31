package jira

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/mcp"
	"github.com/zero4573/tix/internal/mcp/mcptest"
)

func writer(t *testing.T, f mcptest.Fixture) (*Writer, *mcptest.Server) {
	t.Helper()
	s := mcptest.New(f)
	t.Cleanup(s.Close)
	cfg := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(cfg, []byte(s.Config("atlassian")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TIX_MCP_CONFIG", cfg)
	t.Setenv("TIX_MCP_PREPARE", "")
	c, err := mcp.Connect("atlassian")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriter(c, "")
	if err := w.Connect(); err != nil {
		t.Fatal(err)
	}
	return w, s
}

func fixture() mcptest.Fixture {
	return mcptest.Fixture{
		Me:         "acc-1",
		Resources:  json.RawMessage(`[{"cloudId":"c1","url":"https://x.atlassian.net/","products":[{"id":"jira"}]}]`),
		Issues:     map[string]json.RawMessage{"PROJ-7": json.RawMessage(`{"fields":{}}`)},
		IssueTypes: map[string][]string{"PROJ": {"Story", "Bug", "Task"}},
	}
}

func TestCreate(t *testing.T) {
	w, s := writer(t, fixture())
	if w.Me() != "acc-1" {
		t.Fatalf("me = %q", w.Me())
	}
	key, url, err := w.Create(NewIssue{Project: "PROJ", Type: "Story", Summary: "S", Description: "# d", Assignee: w.Me(), Parent: "PROJ-1"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "PROJ-8" || url != "https://x.atlassian.net/browse/PROJ-8" {
		t.Fatalf("got %s %s", key, url)
	}
	last := s.Calls[len(s.Calls)-1]
	for _, want := range []string{"createJiraIssue", `"projectKey":"PROJ"`, `"issueTypeName":"Story"`, `"assignee_account_id":"acc-1"`, `"parent":"PROJ-1"`, `"cloudId":"c1"`} {
		if !strings.Contains(last, want) {
			t.Errorf("call %s lacks %s", last, want)
		}
	}
}

func TestCreateFailure(t *testing.T) {
	f := fixture()
	f.FailCreate = "field 'summary' is required"
	w, _ := writer(t, f)
	_, _, err := w.Create(NewIssue{Project: "PROJ", Type: "Story", Summary: "S"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v", err)
	}
	var u *UnsureError
	if errors.As(err, &u) {
		t.Errorf("a refusal by the server is not unsure: %v", err)
	}
}

func TestCreateLostResponse(t *testing.T) {
	f := fixture()
	f.DropAfterCreate = true
	w, s := writer(t, f)
	_, _, err := w.Create(NewIssue{Project: "PROJ", Type: "Story", Summary: "S"})
	var u *UnsureError
	if !errors.As(err, &u) {
		t.Fatalf("err = %v, want an UnsureError", err)
	}
	if n := strings.Count(strings.Join(s.Calls, "\n"), "createJiraIssue"); n != 1 {
		t.Errorf("createJiraIssue called %d times", n)
	}
}

func TestCheckType(t *testing.T) {
	w, _ := writer(t, fixture())
	if err := w.CheckType("PROJ", "bug"); err != nil {
		t.Fatal(err)
	}
	err := w.CheckType("PROJ", "Epic")
	if err == nil || !strings.Contains(err.Error(), "Story, Bug, Task") {
		t.Fatalf("err = %v", err)
	}
	if err := w.CheckType("NOPE", "Story"); err == nil {
		t.Fatal("unknown project accepted")
	}
}

func TestTypeFor(t *testing.T) {
	for in, want := range map[string]string{"dev": "Story", "bug": "Bug", "chore": "Task", "investigation": "Task", "epic": "Epic", "Dev": "Story", "weird": ""} {
		if got := TypeFor(in); got != want {
			t.Errorf("TypeFor(%q) = %q, want %q", in, got, want)
		}
	}
}
