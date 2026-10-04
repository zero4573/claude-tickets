// Package jira syncs a vault's Jira tickets with no model in the loop,
// through the Atlassian MCP server. Two stages:
//
// Plan: a full reconciliation of the local notes against Jira, from cheap
// listings (key, updated, status):
//
//	create    open in Jira, no note
//	refresh   updated differs from the note's source-updated (or the note
//	          predates the dependency fields); an epic whose children
//	          changed (reason children); a parent epic or a ticket that left
//	          you, when it changed
//	close     local note open, Jira status category done
//	gone      open note whose key Jira no longer returns (deleted, moved,
//	          or no access): tagged unassigned, plus a follow-up task
//	blocked   recomputed from the blockers' status categories
//
// Apply: per ticket, finds what changed and patches only that. The
// changelog since the note's source-updated says which fields moved:
//
//	time tracking, rank, sprint, ... (nothing the note shows)  timestamp only
//	status, assignee, summary, versions, links, parent, ...   frontmatter and
//	                                                           the block's table
//	description (or a text custom field)                       ### Description
//
// and the last comments' ids/timestamps, against the signature kept in the
// note, decide ### Recent comments. New tickets get every part. A
// description or comment that Jira can only give as HTML (panels,
// mentions, media) is handed to Claude (the ticket-sync skill, plan mode)
// for just that part; everything else is written here, deterministically.
package jira

import (
	"encoding/json"
	"fmt"
)

// Changelog fields the note shows (anything else only moves the timestamp)
var tableFields = []string{"summary", "issuetype", "status", "resolution", "priority", "assignee", "reporter",
	"Fix Version", "Component", "labels", "Parent", "IssueParentAssociation", "Epic Link", "Link", "Key"}

// DefaultTextFields are the text custom fields shown under ### Description,
// by label (the source's "textFields" in .sources.json overrides them).
var DefaultTextFields = []string{"QA Testing Instructions", "Acceptance Criteria", "Steps to Reproduce",
	"Expected Result", "Actual Result"}

// Fields fetched for the table and frontmatter
var tableFetch = []string{"summary", "issuetype", "status", "priority", "assignee", "reporter", "fixVersions",
	"components", "labels", "parent", "issuelinks", "subtasks", "updated"}

type named struct {
	Name string `json:"name"`
}

type issueType struct {
	Name           string `json:"name"`
	HierarchyLevel int    `json:"hierarchyLevel"`
}

type status struct {
	Name           string `json:"name"`
	StatusCategory struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

type user struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type linkedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Status *status `json:"status"`
	} `json:"fields"`
}

type fields struct {
	Summary     *string    `json:"summary"`
	Updated     string     `json:"updated"`
	Status      *status    `json:"status"`
	IssueType   *issueType `json:"issuetype"`
	Priority    *named     `json:"priority"`
	Assignee    *user      `json:"assignee"`
	Reporter    *user      `json:"reporter"`
	FixVersions []named    `json:"fixVersions"`
	Components  []named    `json:"components"`
	Labels      []string   `json:"labels"`
	Parent      *struct {
		Key    string `json:"key"`
		Fields struct {
			Summary   *string    `json:"summary"`
			IssueType *issueType `json:"issuetype"`
		} `json:"fields"`
	} `json:"parent"`
	IssueLinks []struct {
		Type struct {
			Name    string `json:"name"`
			Inward  string `json:"inward"`
			Outward string `json:"outward"`
		} `json:"type"`
		InwardIssue  *linkedIssue `json:"inwardIssue"`
		OutwardIssue *linkedIssue `json:"outwardIssue"`
	} `json:"issuelinks"`
	Subtasks []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary *string `json:"summary"`
			Status  *status `json:"status"`
		} `json:"fields"`
	} `json:"subtasks"`
	Description  *string `json:"description"`
	CustomFields map[string]struct {
		Value any `json:"value"`
	} `json:"customFields"`
}

// issue is a Jira issue as the MCP tools return it.
type issue struct {
	Key                  string `json:"key"`
	Fields               fields `json:"fields"`
	AppliedContentFormat string `json:"appliedContentFormat"`
	Changelog            struct {
		Total     int `json:"total"`
		Histories []struct {
			Created string `json:"created"`
			Items   []struct {
				Field string `json:"field"`
			} `json:"items"`
		} `json:"histories"`
	} `json:"changelog"`
}

func (f fields) cat() string {
	if f.Status == nil || f.Status.StatusCategory.Key == "" {
		return ""
	}
	return f.Status.StatusCategory.Key
}

// comment is one of the last comments.
type comment struct {
	ID      any    `json:"id"`
	Created string `json:"created"`
	Updated any    `json:"updated"`
	Body    string `json:"body"`
	Author  *user  `json:"author"`
}

type comments struct {
	Comments             []comment `json:"comments"`
	AppliedContentFormat string    `json:"appliedContentFormat"`
}

// str is jq's string interpolation of a JSON value.
func str(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func or(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// Child is a row of an epic's children.
type Child struct {
	Epic     string `json:"epic"`
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Type     string `json:"type"`
	Assignee string `json:"assignee"`
	Mine     bool   `json:"mine"`
	Status   string `json:"status"`
	Cat      string `json:"cat"`
	Sig      string `json:"sig"`
}

// Refresh is a note to bring up to date, and why.
type Refresh struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Flip is a note whose blocked changes.
type Flip struct {
	ID      string `json:"id"`
	Blocked bool   `json:"blocked"`
}

// Plan is what a sync does to the notes.
type Plan struct {
	Source       string              `json:"source"`
	CloudID      string              `json:"cloudId"`
	AccountID    string              `json:"accountId"`
	Create       []string            `json:"create"`
	Refresh      []Refresh           `json:"refresh"`
	Close        []string            `json:"close"`
	Gone         []string            `json:"gone"`
	Blocked      []Flip              `json:"blocked"`
	EpicChildren map[string][]string `json:"epicChildren"`
	Counts       struct {
		Open    int `json:"open"`
		Skipped int `json:"skipped"`
	} `json:"counts"`
}

// HTMLPart is a part of a note only Claude can write (Jira gives it as
// HTML only).
type HTMLPart struct {
	ID     string `json:"id"`
	Part   string `json:"part"`
	Marker string `json:"marker,omitempty"`
}

func (p Plan) String() string {
	return fmt.Sprintf("%s: open %d, new %d, to refresh %d, to close %d, gone %d, blocked flips %d, unchanged %d",
		p.Source, p.Counts.Open, len(p.Create), len(p.Refresh), len(p.Close), len(p.Gone), len(p.Blocked), p.Counts.Skipped)
}
