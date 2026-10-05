package jira

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/note"
)

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "|", `\|`)
}

var plainYAML = regexp.MustCompile("^[^\\[\\]{}#&*!|>%@`\"',?:-][^#\"]*$")

// yamlStr is s as a YAML scalar: plain when that's safe, else JSON-quoted.
func yamlStr(s string) string {
	if plainYAML.MatchString(s) && !strings.Contains(s, ": ") && !strings.HasSuffix(s, ":") {
		return s
	}
	return toJSON(s)
}

// toJSON is jq's tojson: compact, no HTML escaping.
func toJSON(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func link(k string) string { return `"[[` + k + `]]"` }

func links(keys []string) string {
	l := make([]string, len(keys))
	for i, k := range keys {
		l[i] = link(k)
	}
	return "[" + strings.Join(l, ", ") + "]"
}

func category(key string) string {
	switch key {
	case "indeterminate":
		return "in-progress"
	case "done":
		return "done"
	default:
		return "todo"
	}
}

var (
	investigationRe = regexp.MustCompile(`^(Spike|Investigation|Research)$`)
	devRe           = regexp.MustCompile(`^(Story|Task|Improvement|New Feature|Sub-task|Subtask)$`)
	projectRe       = regexp.MustCompile(`[^a-z0-9]+`)
)

func isEpic(t *issueType) bool {
	return t != nil && (t.Name == "Epic" || t.HierarchyLevel >= 1)
}

func ticketType(t *issueType) string {
	name := ""
	if t != nil {
		name = t.Name
	}
	switch {
	case isEpic(t):
		return "epic"
	case name == "Bug":
		return "bug"
	case investigationRe.MatchString(name):
		return "investigation"
	case devRe.MatchString(name):
		return "dev"
	default:
		return "chore"
	}
}

// trimSpace is jq's sub("\\s+$"; "").
func trimSpace(s string) string { return strings.TrimRight(s, " \t\n\v\f\r") }

func names(ns []named) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Name
	}
	return out
}

func statusName(s *status, def string) string {
	if s == nil || s.Name == "" {
		return def
	}
	return s.Name
}

func (s *Sync) head(is issue) string {
	f := is.Fields
	none := func(l []string, empty string) string {
		if len(l) == 0 {
			return empty
		}
		return strings.Join(l, ", ")
	}
	typ, prio, assignee, reporter := "?", "none", "Unassigned", "unknown"
	if f.IssueType != nil && f.IssueType.Name != "" {
		typ = f.IssueType.Name
	}
	if f.Priority != nil && f.Priority.Name != "" {
		prio = f.Priority.Name
	}
	if f.Assignee != nil && f.Assignee.DisplayName != "" {
		assignee = f.Assignee.DisplayName
	}
	if f.Reporter != nil && f.Reporter.DisplayName != "" {
		reporter = f.Reporter.DisplayName
	}
	ls := []string{
		"## Source (Jira)",
		"> Synced from Jira by ct sync. Edits here are overwritten. Dashboard: [[tickets.base|Tickets]]",
		"",
		"| | |", "|---|---|",
		"| Summary | " + cell(or(f.Summary, "")) + " |",
		"| Type / priority | " + cell(typ) + " / " + cell(prio) + " |",
		"| Status | " + cell(statusName(f.Status, "?")) + " |",
		"| Assignee / reporter | " + cell(assignee) + " / " + cell(reporter) + " |",
		"| Fix versions | " + cell(none(names(f.FixVersions), "none set")) + " |",
		"| Components / labels | " + cell(none(names(f.Components), "none")) + " / " + cell(none(f.Labels, "none")) + " |",
	}
	if f.Parent != nil {
		ls = append(ls, "| Parent | [["+f.Parent.Key+"]] ("+cell(or(f.Parent.Fields.Summary, ""))+") |")
	}
	if len(f.Subtasks) > 0 {
		var sub []string
		for _, t := range f.Subtasks {
			sub = append(sub, "[["+t.Key+"]] ("+cell(or(t.Fields.Summary, ""))+", "+cell(statusName(t.Fields.Status, "?"))+")")
		}
		ls = append(ls, "| Subtasks | "+strings.Join(sub, "; ")+" |")
	}
	if len(f.IssueLinks) > 0 {
		var ln []string
		for _, l := range f.IssueLinks {
			if l.InwardIssue != nil {
				ln = append(ln, l.Type.Inward+" [["+l.InwardIssue.Key+"]]")
			} else if l.OutwardIssue != nil {
				ln = append(ln, l.Type.Outward+" [["+l.OutwardIssue.Key+"]]")
			} else {
				ln = append(ln, l.Type.Outward+" [[null]]")
			}
		}
		ls = append(ls, "| Links | "+cell(strings.Join(ln, ", "))+" |")
	}
	ls = append(ls, "| URL | "+s.url+"/browse/"+is.Key+" |")
	return strings.Join(ls, "\n") + "\n"
}

// fmField is a frontmatter field the sync owns.
type fmField struct{ key, value string }

// frontmatter is the fields the sync owns, plus what the applier needs:
// whether it's yours, its ticket type, project tag and parent epic.
func (s *Sync) frontmatter(is issue) (fs []fmField, mine bool, typ, project, parentEpic string) {
	f := is.Fields
	var by, blocks, related []string
	blocked := false
	seen := map[string]bool{}
	for _, l := range f.IssueLinks {
		if l.Type.Name == "Blocks" && l.InwardIssue != nil {
			by = append(by, l.InwardIssue.Key)
			cat := "new"
			if l.InwardIssue.Fields.Status != nil && l.InwardIssue.Fields.Status.StatusCategory.Key != "" {
				cat = l.InwardIssue.Fields.Status.StatusCategory.Key
			}
			blocked = blocked || cat != "done"
		}
		if l.Type.Name == "Blocks" && l.OutwardIssue != nil {
			blocks = append(blocks, l.OutwardIssue.Key)
		}
		if l.Type.Name != "Blocks" {
			k := "null"
			if l.InwardIssue != nil {
				k = l.InwardIssue.Key
			} else if l.OutwardIssue != nil {
				k = l.OutwardIssue.Key
			}
			if !seen[k] {
				seen[k] = true
				related = append(related, k)
			}
		}
	}
	sort.Strings(related)
	if related == nil {
		related = []string{}
	}
	typName, statusName, statusCat, prio, assignee := "", "", "new", "", "Unassigned"
	if f.IssueType != nil {
		typName = f.IssueType.Name
	}
	if f.Status != nil {
		statusName = f.Status.Name
		if f.Status.StatusCategory.Key != "" {
			statusCat = f.Status.StatusCategory.Key
		}
	}
	if f.Priority != nil {
		prio = f.Priority.Name
	}
	if f.Assignee != nil && f.Assignee.DisplayName != "" {
		assignee = f.Assignee.DisplayName
	}
	parent := ""
	if f.Parent != nil {
		parent = link(f.Parent.Key)
		if isEpic(f.Parent.Fields.IssueType) && note.ValidKey(f.Parent.Key) {
			parentEpic = f.Parent.Key
		}
	}
	versions := names(f.FixVersions)
	fs = []fmField{
		{"summary", yamlStr(or(f.Summary, ""))},
		{"source-type", yamlStr(typName)},
		{"source-status", yamlStr(statusName)},
		{"source-status-category", category(statusCat)},
		{"source-priority", yamlStr(prio)},
		{"source-assignee", yamlStr(assignee)},
		{"source-fix-versions", toJSON(versions)},
		{"parent", parent},
		{"blocked-by", links(by)},
		{"blocks", links(blocks)},
		{"related", links(related)},
		{"blocked", fmt.Sprint(blocked)},
	}
	acc := ""
	if f.Assignee != nil {
		acc = f.Assignee.AccountID
	}
	project = projectRe.ReplaceAllString(strings.ToLower(strings.Split(is.Key, "-")[0]), "-")
	return fs, acc == s.me, ticketType(f.IssueType), project, parentEpic
}

// description renders ### Description from the evidence view.
func (s *Sync) description(is issue) string {
	d := trimSpace(or(is.Fields.Description, ""))
	if d == "" {
		d = "_No description._"
	}
	ls := []string{"### Description", d}
	for _, label := range s.TextFields {
		v, ok := is.Fields.CustomFields[label].Value.(string)
		if ok && strings.TrimSpace(v) != "" && regexp.MustCompile(`\S`).MatchString(v) {
			ls = append(ls, "", "**"+label+":** "+trimSpace(v))
		}
	}
	return strings.Join(ls, "\n") + "\n"
}

// commentSig is the marker that says which comments a note shows.
func commentSig(cs []comment) string {
	if len(cs) == 0 {
		return "<!-- comments: none -->"
	}
	var ids []string
	for _, c := range cs {
		ids = append(ids, str(c.ID)+"@"+str(c.Updated))
	}
	return "<!-- comments: " + strings.Join(ids, ",") + " -->"
}

func renderComments(cs []comment, sig string) string {
	ls := []string{"### Recent comments"}
	if len(cs) == 0 {
		ls = append(ls, "_No comments._")
	}
	for _, c := range cs {
		body := strings.Split(trimSpace(c.Body), "\n")
		author := "unknown"
		if c.Author != nil && c.Author.DisplayName != "" {
			author = c.Author.DisplayName
		}
		date := c.Created
		if len(date) > 10 {
			date = date[:10]
		}
		ls = append(ls, "- **"+author+"**, "+date+": "+body[0])
		for i := 1; i < len(body) && i < 15; i++ {
			if body[i] == "" {
				ls = append(ls, "")
			} else {
				ls = append(ls, "  "+body[i])
			}
		}
	}
	return strings.Join(append(ls, sig), "\n") + "\n"
}

var commentMarker = regexp.MustCompile(`<!-- comments: .* -->`)

// writeChildren writes an epic's children part and its children / covers
// fields, plus covered-by on the children's own notes.
func (s *Sync) writeChildren(epic, file string, p *parts) error {
	var rows []Child
	for _, r := range s.Children {
		if r.Epic == epic {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		got, err := s.children([]string{epic})
		if err != nil {
			return err
		}
		rows = got
		s.Children = append(s.Children, got...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	p.children = ""
	if len(rows) > 0 {
		ls := []string{"### Children", "| Ticket | Summary | Type | Assignee | Status |", "|---|---|---|---|---|"}
		for _, r := range rows {
			who := ""
			if !r.Mine {
				who = " *(not yours)*"
			}
			ls = append(ls, fmt.Sprintf("| [[%s]]%s | %s | %s | %s | %s |", r.Key, who, cell(r.Summary), cell(r.Type), cell(r.Assignee), cell(r.Status)))
		}
		p.children = strings.Join(ls, "\n") + "\n"
	}
	var keys, covers []string
	covered := map[string]bool{}
	for _, r := range rows {
		keys = append(keys, r.Key)
		if r.Mine && r.Cat != "done" {
			covers = append(covers, r.Key)
			covered[r.Key] = true
		}
	}
	sort.Strings(keys)
	sort.Strings(covers)
	if err := note.Set(file, "children", links(keys)); err != nil {
		return err
	}
	if err := note.Set(file, "covers", links(covers)); err != nil {
		return err
	}
	// covered-by on the covered children's notes (never over a manual lead)
	for _, r := range rows {
		if !r.Mine || r.Cat == "done" {
			continue
		}
		cn := s.notePath(r.Key)
		if _, err := os.Stat(cn); err != nil || frontmatterGet(cn, "source") != "jira" {
			continue
		}
		cur := frontmatterGet(cn, "covered-by")
		if strings.HasPrefix(cur, "[[MAN-") {
			continue
		}
		if cur != "[["+epic+"]]" {
			_ = note.Set(cn, "covered-by", link(epic))
		}
	}
	// and off the notes that left it
	files, _ := filepath.Glob(filepath.Join(s.Vault, "tickets", "*", "*.md"))
	sort.Strings(files)
	for _, cn := range files {
		data, _ := os.ReadFile(cn)
		if !strings.Contains("\n"+note.Normalize(string(data)), "\ncovered-by: \"[["+epic+"]]\"") {
			continue
		}
		if !covered[strings.TrimSuffix(filepath.Base(cn), ".md")] {
			_ = note.Set(cn, "covered-by", "")
		}
	}
	return nil
}

// frontmatterGet is a frontmatter field as the bash tools read it: the
// first "key:" line, its value with a leading quote and a trailing quote
// (and trailing blanks) dropped.
func frontmatterGet(file, key string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	ls := strings.Split(note.Normalize(string(data)), "\n")
	if len(ls) == 0 || ls[0] != "---" {
		return ""
	}
	for _, l := range ls[1:] {
		if l == "---" {
			break
		}
		k, v, _ := strings.Cut(l, ":")
		if k != key {
			continue
		}
		v = strings.TrimLeft(v, " \t")
		if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
			v = v[1:]
		}
		if t := strings.TrimRight(v, " \t"); len(t) > 0 && (t[len(t)-1] == '"' || t[len(t)-1] == '\'') {
			v = t[:len(t)-1]
		}
		return v
	}
	return ""
}

var unassignedTag = regexp.MustCompile(`(?m)^tags:.*[\[ ,]unassigned[\],]`)
var parentEpicTag = regexp.MustCompile(`(?m)^tags:.*parent-epic`)

// apply brings one note up to date: mode create, refresh or close.
func (s *Sync) apply(key, mode, reason string) error {
	file := s.notePath(key)
	created := false
	newStatus, reopened := "", false
	var p parts
	all, table, desc := false, false, false
	var changes []string
	updated := ""

	if mode == "create" {
		tpl, err := os.ReadFile(filepath.Join(s.Vault, "templates", "ticket-synced.md"))
		if err != nil {
			return err
		}
		t := strings.ReplaceAll(strings.ReplaceAll(string(tpl), "{{title}}", key), "{{date}}", s.today)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		// Never over an existing note: one the index can't read (no jira
		// frontmatter) still holds the user's work
		f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("%s exists but isn't a jira note ct sync can read (check its frontmatter: source: jira, source-id: %s)", file, key)
			}
			return err
		}
		_, err = f.WriteString(t)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(file)
			return err
		}
		// A create that fails later leaves no half-written note behind
		defer func() {
			if !created {
				_ = os.Remove(file)
			}
		}()
		all = true
	} else if got, ok := splitBlock(file); ok {
		p = got
	} else {
		all = true
	}

	// What moved since the note's last sync
	since := frontmatterGet(file, "source-updated")
	switch {
	case !all && reason == "children":
		// only the children part, below
	case !all && since != "":
		ch, err := s.getIssue(key, map[string]any{"fields": []string{"updated"}, "fieldsByKeys": true, "expand": "changelog"})
		if err != nil {
			return err
		}
		updated = ch.Fields.Updated
		var fields []string
		seen := map[string]bool{}
		for _, h := range ch.Changelog.Histories {
			if h.Created <= since {
				continue
			}
			for _, it := range h.Items {
				f := it.Field
				inTable := has(tableFields, f)
				inText := has(s.TextFields, f)
				if inTable {
					table = true
				}
				if f == "description" || inText {
					desc = true
				}
				if (inTable || inText || f == "description") && !seen[f] {
					seen[f] = true
					fields = append(fields, f)
				}
			}
		}
		hs := ch.Changelog.Histories
		if ch.Changelog.Total > len(hs) && len(hs) > 0 && hs[len(hs)-1].Created > since {
			all = true
		}
		sort.Strings(fields)
		if len(fields) > 0 {
			changes = append(changes, strings.Join(fields, ","))
		}
	default:
		all = true
	}
	// Reasons that need the current fields whatever the history says
	switch reason {
	case "unassigned", "dependencies", "parent-epic":
		table = true
	}
	if mode == "close" {
		table = true
	}
	if all {
		table, desc = true, true
	}

	if table {
		is, err := s.getIssue(key, map[string]any{"fields": tableFetch, "fieldsByKeys": true, "view": "full"})
		if err != nil {
			return err
		}
		updated = is.Fields.Updated
		p.head = s.head(is)
		fs, mine, typ, project, pepic := s.frontmatter(is)
		for _, f := range fs {
			if err := note.Set(file, f.key, f.value); err != nil {
				return err
			}
		}
		catNow := frontmatterGet(file, "source-status-category")
		statusNow := frontmatterGet(file, "status")
		if mode == "create" {
			for _, f := range []fmField{{"source", "jira"}, {"source-id", key}, {"source-url", s.url + "/browse/" + key},
				{"ticket-type", typ}, {"status", "new"}, {"created", s.today}} {
				_ = note.Set(file, f.key, f.value)
			}
			_ = note.EditTags(file, true, "jira")
			_ = note.EditTags(file, true, project)
			if reason == "parent-epic" {
				_ = note.EditTags(file, true, "parent-epic")
			}
		}
		// Written last (see the end): a note closed before its other parts are
		// written would never be retried, since the planner skips closed notes
		statusAfter := statusNow
		if mode == "close" || catNow == "done" {
			if statusNow != "closed" {
				newStatus, statusAfter = "closed", "closed"
				changes = append(changes, "closed")
			}
		} else if statusNow == "closed" {
			newStatus, statusAfter, reopened = "new", "new", true
			changes = append(changes, "reopened")
		}
		data, _ := os.ReadFile(file)
		if mine {
			_ = note.EditTags(file, false, "unassigned")
		} else if statusAfter != "closed" && reason != "parent-epic" && !parentEpicTag.Match(data) {
			if !unassignedTag.Match(data) {
				changes = append(changes, "no longer yours")
			}
			_ = note.EditTags(file, true, "unassigned")
		}
		// A parent epic with no note is pulled in (tagged parent-epic)
		if pepic != "" {
			if _, err := os.Stat(s.notePath(pepic)); err != nil {
				s.parents = append(s.parents, pepic)
			}
		}
	}

	// Description (and text custom fields)
	if desc {
		ev, err := s.getIssue(key, map[string]any{"view": "evidence"})
		if err != nil {
			return err
		}
		if f := ev.AppliedContentFormat; f != "" && f != "markdown" {
			s.HTML = append(s.HTML, HTMLPart{ID: key, Part: "description"})
			changes = append(changes, "description (HTML, to Claude)")
		} else {
			p.description = s.description(ev)
		}
	}

	// Comments: rewritten when the last five's ids or timestamps changed
	if reason != "children" || all {
		var cm comments
		if err := s.tool("executeRead", map[string]any{"name": "listJiraIssueComments", "cloudId": s.cloud,
			"inputs": map[string]any{"issueIdOrKey": key, "maxResults": 5, "orderBy": "-created"}}, &cm); err != nil {
			return err
		}
		sort.SliceStable(cm.Comments, func(i, j int) bool { return cm.Comments[i].Created < cm.Comments[j].Created })
		sig := commentSig(cm.Comments)
		if sig != commentMarker.FindString(p.comments) {
			if f := cm.AppliedContentFormat; f != "" && f != "markdown" {
				s.HTML = append(s.HTML, HTMLPart{ID: key, Part: "comments", Marker: sig})
				changes = append(changes, "comments (HTML, to Claude)")
			} else {
				p.comments = renderComments(cm.Comments, sig)
				if !all {
					changes = append(changes, "comments")
				}
			}
		}
	}

	if frontmatterGet(file, "ticket-type") == "epic" && (table || reason == "children") {
		if err := s.writeChildren(key, file, &p); err != nil {
			return err
		}
		if reason == "children" {
			changes = append(changes, "children")
		}
	}

	if err := writeBlock(file, p); err != nil {
		return err
	}
	if newStatus != "" {
		_ = note.Set(file, "status", newStatus)
	}
	if reopened {
		s.Followup("[[" + key + "]] was reopened in Jira (status: new again); check whether work should resume")
	}
	if updated != "" {
		_ = note.Set(file, "source-updated", updated)
	}
	_ = note.Set(file, "updated", s.today)

	what := strings.Join(changes, "; ")
	switch {
	case mode == "create":
		what = "new"
	case mode == "close":
		what = "closed"
	case len(changes) == 0:
		what = "nothing the note shows (timestamp only)"
	}
	created = true
	s.Say(fmt.Sprintf("%s: %s [%s] %s", key, what, frontmatterGet(file, "source-status"), frontmatterGet(file, "summary")))
	return nil
}

// Apply works through the plan, then the parent epics it pulled in.
// Failures become follow-up tasks; it returns the tickets that failed.
func (s *Sync) Apply() (failed []string) {
	type step struct{ key, mode, reason string }
	var steps []step
	for _, k := range s.Plan.Create {
		steps = append(steps, step{k, "create", ""})
	}
	for _, k := range s.Plan.Close {
		steps = append(steps, step{k, "close", ""})
	}
	for _, r := range s.Plan.Refresh {
		steps = append(steps, step{r.ID, "refresh", r.Reason})
	}
	for _, st := range steps {
		if st.key == "" {
			continue
		}
		if err := s.apply(st.key, st.mode, st.reason); err != nil {
			s.Log(err.Error())
			failed = append(failed, st.key)
			s.Say(st.key + ": couldn't sync (see the messages above)")
			s.Followup("Check [[" + st.key + "]]: ticket-sync couldn't update it; see " + s.LogPath)
		}
	}
	parents := append([]string{}, s.parents...)
	sort.Strings(parents)
	for i, k := range parents {
		if i > 0 && parents[i-1] == k {
			continue
		}
		if _, err := os.Stat(s.notePath(k)); err == nil {
			continue
		}
		if err := s.apply(k, "create", "parent-epic"); err != nil {
			s.Log(err.Error())
			failed = append(failed, k)
			s.Say(k + ": couldn't pull in the parent epic")
			s.Followup("Check [[" + k + "]]: ticket-sync couldn't create this parent epic's note")
		}
	}
	return failed
}
