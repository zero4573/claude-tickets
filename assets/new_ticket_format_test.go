package assets

import (
	"strings"
	"testing"
)

// Jira task lists are filed as HTML task lists, not markdown checkboxes.
func TestNewTicketFormatJiraTaskLists(t *testing.T) {
	text := retroText(t, "plugin/skills/new-ticket/SKILL.md")
	for _, want := range []string{
		`contentFormat: "html"`,
		`data-type="task-list"`,
		`data-type="task-item"`,
		`<input type="checkbox" checked>`,
		"`editJiraIssue`",
		"`&amp;`, `&lt;`, `&gt;`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("new-ticket skill missing %q", want)
		}
	}
}
