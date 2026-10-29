package jira

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/zero4573/claude-tickets/internal/mcp"
)

// Writer files issues at a Jira site through the Atlassian MCP server.
// Only creation lives here: edits and transitions go through the
// session's own MCP tools.
type Writer struct {
	c     *mcp.Client
	cloud string
	url   string
	me    string
}

// NewWriter is a Writer over c for site (empty: the token's only Jira
// site). Call Connect before Create.
func NewWriter(c *mcp.Client, site string) *Writer {
	w := &Writer{c: c}
	if site != "" {
		w.cloud = site
		w.url = "https://" + strings.TrimPrefix(site, "https://")
	}
	return w
}

func (w *Writer) tool(name string, args map[string]any, v any) error {
	raw, err := w.c.Tool(name, args)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var d struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if v == nil || len(d.Data) == 0 || string(d.Data) == "null" {
		return nil
	}
	return json.Unmarshal(d.Data, v)
}

// Connect picks the Jira site and reads your account id.
func (w *Writer) Connect() error {
	if w.cloud == "" {
		var res struct {
			Resources []struct {
				CloudID  string `json:"cloudId"`
				URL      string `json:"url"`
				Products []struct {
					ID string `json:"id"`
				} `json:"products"`
			} `json:"resources"`
		}
		if err := w.tool("getAccessibleAtlassianResources", map[string]any{}, &res); err != nil {
			return err
		}
		n := 0
		for _, r := range res.Resources {
			if slices.ContainsFunc(r.Products, func(p struct {
				ID string `json:"id"`
			}) bool {
				return p.ID == "jira"
			}) {
				n++
				w.cloud, w.url = r.CloudID, r.URL
			}
		}
		if n != 1 {
			return fmt.Errorf("can't pick a Jira site (none, or several, reachable): set \"site\" in tickets/.sources.json")
		}
	}
	w.url = strings.TrimSuffix(w.url, "/")
	var u struct {
		AccountID string `json:"accountId"`
	}
	if err := w.tool("atlassianUserInfo", map[string]any{}, &u); err != nil {
		return err
	}
	if u.AccountID == "" {
		return fmt.Errorf("couldn't read your Jira account id (atlassianUserInfo)")
	}
	w.me = u.AccountID
	return nil
}

// Me is your Jira account id (after Connect).
func (w *Writer) Me() string { return w.me }

// IssueTypes are the issue type names of project.
func (w *Writer) IssueTypes(project string) ([]string, error) {
	var out struct {
		IssueTypes []struct {
			Name string `json:"name"`
		} `json:"issueTypes"`
	}
	if err := w.tool("getJiraProjectIssueTypesMetadata", map[string]any{"cloudId": w.cloud, "projectIdOrKey": project}, &out); err != nil {
		return nil, err
	}
	var names []string
	for _, t := range out.IssueTypes {
		names = append(names, t.Name)
	}
	return names, nil
}

// CheckType is nil when project has an issue type named typ, else an
// error listing the project's types.
func (w *Writer) CheckType(project, typ string) error {
	names, err := w.IssueTypes(project)
	if err != nil {
		return err
	}
	for _, n := range names {
		if strings.EqualFold(n, typ) {
			return nil
		}
	}
	return fmt.Errorf("project %s has no issue type %q (it has: %s)", project, typ, strings.Join(names, ", "))
}

// NewIssue is what Create files. Description is markdown. Assignee is an
// account id (empty: unassigned); Parent an issue key (empty: none).
type NewIssue struct {
	Project, Type, Summary, Description, Assignee, Parent string
}

// UnsureError is a Create that failed without a server answer saying so (a
// dropped connection, a timeout, an answer that couldn't be read): the issue
// may exist already, so filing again could make a second one.
type UnsureError struct{ Err error }

func (e *UnsureError) Error() string { return e.Err.Error() }
func (e *UnsureError) Unwrap() error { return e.Err }

// serverRefused reports whether err is the server's own refusal of a tool
// call (a tool error or a JSON-RPC error), as opposed to a failure to get
// or read its answer.
func serverRefused(name string, err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "MCP tool "+name+" failed:") || strings.Contains(msg, "MCP tools/call: {")
}

// Create files the issue and returns its key and browse URL. An error that
// the server didn't answer as a refusal is an *UnsureError.
func (w *Writer) Create(in NewIssue) (key, url string, err error) {
	args := map[string]any{
		"cloudId": w.cloud, "projectKey": in.Project, "issueTypeName": in.Type,
		"summary": in.Summary, "contentFormat": "markdown",
	}
	if in.Description != "" {
		args["description"] = in.Description
	}
	if in.Assignee != "" {
		args["assignee_account_id"] = in.Assignee
	}
	if in.Parent != "" {
		args["parent"] = in.Parent
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := w.tool("createJiraIssue", args, &out); err != nil {
		if serverRefused("createJiraIssue", err) {
			return "", "", err
		}
		return "", "", &UnsureError{err}
	}
	if out.Key == "" {
		return "", "", &UnsureError{fmt.Errorf("createJiraIssue: no issue key in the answer")}
	}
	return out.Key, w.url + "/browse/" + out.Key, nil
}

// TypeFor is the default Jira issue type of a ticket's type (empty for an
// unknown one: the caller asks for --type).
func TypeFor(ticketType string) string {
	switch strings.ToLower(strings.TrimSpace(ticketType)) {
	case "dev":
		return "Story"
	case "bug":
		return "Bug"
	case "chore", "investigation":
		return "Task"
	case "epic":
		return "Epic"
	}
	return ""
}
