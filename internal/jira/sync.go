package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/mcp"
	"github.com/zero4573/claude-tickets/internal/note"
)

// ErrReported is a failure already reported through Say (and Followup).
var ErrReported = errors.New("reported")

// Sync is one run over one Jira source of a vault.
type Sync struct {
	Vault  string
	Source string
	// Site is the source's "site" (empty: the token's only Jira site)
	Site string
	// Query is the JQL of the open tickets
	Query string
	// TextFields are the text custom fields shown under ### Description
	TextFields []string
	// KnownChildren is the epics' children recorded by the last planned
	// run (nil: none yet)
	KnownChildren map[string][]string
	// Say reports a line (also kept for the summary); Followup leaves a
	// task for the follow-up note; Log is progress for stderr
	Say, Followup, Log func(string)
	// LogName is the log's file name, for follow-up tasks
	LogName string

	c         *mcp.Client
	cloud, me string
	url       string
	today     string

	Plan     Plan
	Children []Child
	HTML     []HTMLPart
	parents  []string
}

// New prepares a sync over the MCP client c.
func (s *Sync) New(c *mcp.Client) *Sync {
	s.c = c
	s.today = time.Now().Format("2006-01-02")
	if s.TextFields == nil {
		s.TextFields = DefaultTextFields
	}
	return s
}

func (s *Sync) notePath(id string) string { return note.TicketPath(s.Vault, id) }

// tool calls an MCP tool and decodes its "data" into v.
func (s *Sync) tool(name string, args map[string]any, v any) error {
	raw, err := s.c.Tool(name, args)
	if err != nil {
		return err
	}
	var w struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	if v == nil || len(w.Data) == 0 || string(w.Data) == "null" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(w.Data)))
	dec.UseNumber()
	return dec.Decode(v)
}

// search is every issue matching jql, page by page.
func (s *Sync) search(jql string, fields []string) ([]issue, error) {
	var all []issue
	token := ""
	for {
		args := map[string]any{"cloudId": s.cloud, "jql": jql, "fields": fields, "maxResults": 100, "view": "full"}
		if token != "" {
			args["nextPageToken"] = token
		}
		var page struct {
			Issues        []issue `json:"issues"`
			IsLast        *bool   `json:"isLast"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := s.tool("searchJiraIssuesUsingJql", args, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Issues...)
		if (page.IsLast != nil && *page.IsLast) || page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}

// byKeys is those issues (missing keys are left out, as Jira does), 50 at
// a time.
func (s *Sync) byKeys(fields []string, keys []string) ([]issue, error) {
	var all []issue
	for len(keys) > 0 {
		n := min(50, len(keys))
		got, err := s.search("key in ("+strings.Join(keys[:n], ",")+")", fields)
		if err != nil {
			return nil, err
		}
		all = append(all, got...)
		keys = keys[n:]
	}
	return all, nil
}

func (s *Sync) getIssue(key string, extra map[string]any) (issue, error) {
	args := map[string]any{"cloudId": s.cloud, "issueIdOrKey": key}
	for k, v := range extra {
		args[k] = v
	}
	var is issue
	err := s.tool("getJiraIssue", args, &is)
	return is, err
}

// children is one row per child of the epics, every assignee.
func (s *Sync) children(epics []string) ([]Child, error) {
	var rows []Child
	for len(epics) > 0 {
		n := min(50, len(epics))
		got, err := s.search("parent in ("+strings.Join(epics[:n], ",")+")",
			[]string{"summary", "issuetype", "status", "assignee", "parent"})
		if err != nil {
			return nil, err
		}
		epics = epics[n:]
		for _, is := range got {
			f := is.Fields
			acc := ""
			if f.Assignee != nil {
				acc = f.Assignee.AccountID
			}
			mine := acc == s.me
			r := Child{Key: is.Key, Summary: str(ptrAny(f.Summary)), Mine: mine, Assignee: "Unassigned"}
			if f.Parent != nil {
				r.Epic = f.Parent.Key
			}
			if f.IssueType != nil {
				r.Type = f.IssueType.Name
			} else {
				r.Type = "null"
			}
			if f.Assignee != nil && f.Assignee.DisplayName != "" {
				r.Assignee = f.Assignee.DisplayName
			}
			if f.Status != nil {
				r.Status, r.Cat = f.Status.Name, f.Status.StatusCategory.Key
			} else {
				r.Status, r.Cat = "null", "null"
			}
			who := "other"
			if mine {
				who = "mine"
			}
			r.Sig = r.Key + ":" + r.Cat + ":" + who
			rows = append(rows, r)
		}
	}
	return rows, nil
}

func ptrAny(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// childSigs groups children rows by epic: each epic's sorted signatures.
func childSigs(rows []Child) map[string][]string {
	out := map[string][]string{}
	for _, r := range rows {
		out[r.Epic] = append(out[r.Epic], r.Sig)
	}
	for _, v := range out {
		sort.Strings(v)
	}
	return out
}

// ChildSigs is the epics' children signatures of every row seen this run.
func (s *Sync) ChildSigs() map[string][]string { return childSigs(s.Children) }

// local is a jira note, as the planner sees it.
type local struct {
	id, updated, status, typ string
	tags                     []string
	hasBlockedBy             bool
	blocked                  string
	blockers                 []string
}

var blockerRe = regexp.MustCompile(`\[\[([A-Z][A-Z0-9_]*-[A-Z0-9-]+)\]\]`)

func (s *Sync) localIndex() []local {
	files, _ := filepath.Glob(filepath.Join(s.Vault, "tickets", "*", "*.md"))
	sort.Strings(files)
	var out []local
	for _, f := range files {
		fm := note.Fields(f)
		if fm["source"] != "jira" {
			continue
		}
		l := local{id: fm["source-id"], updated: fm["source-updated"], status: fm["status"], typ: fm["ticket-type"], blocked: fm["blocked"]}
		if l.id == "" {
			l.id = filepath.Base(filepath.Dir(f))
		}
		if t := strings.TrimSuffix(strings.TrimPrefix(fm["tags"], "["), "]"); t != "" {
			for _, x := range strings.Split(t, ",") {
				l.tags = append(l.tags, strings.Trim(x, " "))
			}
		}
		_, l.hasBlockedBy = fm["blocked-by"]
		for _, m := range blockerRe.FindAllStringSubmatch(fm["blocked-by"], -1) {
			l.blockers = append(l.blockers, m[1])
		}
		out = append(out, l)
	}
	return out
}

func has(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Connect picks the Jira site and reads who you are. A false return has
// been reported (Say, Followup).
func (s *Sync) Connect() error {
	if s.Site != "" {
		s.cloud = s.Site
		s.url = "https://" + strings.TrimPrefix(s.Site, "https://")
	} else {
		var res struct {
			Resources []struct {
				CloudID  string `json:"cloudId"`
				URL      string `json:"url"`
				Products []struct {
					ID string `json:"id"`
				} `json:"products"`
			} `json:"resources"`
		}
		err := s.tool("getAccessibleAtlassianResources", map[string]any{}, &res)
		var jira []int
		for i, r := range res.Resources {
			for _, p := range r.Products {
				if p.ID == "jira" {
					jira = append(jira, i)
					break
				}
			}
		}
		if err != nil || len(jira) != 1 {
			if err != nil {
				s.Log(err.Error())
			}
			s.Say(s.Source + ": can't pick a Jira site (none, or several, reachable): set \"site\" in tickets/.sources.json")
			s.Followup("ticket-sync " + s.Source + ": set \"site\" for the " + s.Source + " source in tickets/.sources.json (the token reaches no single Jira site)")
			return ErrReported
		}
		s.cloud, s.url = res.Resources[jira[0]].CloudID, res.Resources[jira[0]].URL
	}
	s.url = strings.TrimSuffix(s.url, "/")
	raw, err := s.c.Tool("atlassianUserInfo", map[string]any{})
	if err == nil {
		var u struct {
			Data struct {
				AccountID string `json:"accountId"`
			} `json:"data"`
			AccountID string `json:"accountId"`
		}
		if json.Unmarshal(raw, &u) == nil {
			s.me = u.Data.AccountID
			if s.me == "" {
				s.me = u.AccountID
			}
		}
	} else {
		s.Log(err.Error())
	}
	if s.me == "" {
		s.Say(s.Source + ": couldn't read your Jira account id (atlassianUserInfo)")
		return ErrReported
	}
	if s.Query == "" {
		s.Say(s.Source + ": no \"query\" in tickets/.sources.json")
		s.Followup("ticket-sync " + s.Source + ": set \"query\" in tickets/.sources.json")
		return ErrReported
	}
	return nil
}

// MakePlan reconciles the notes with Jira, and writes what needs no
// fetching (gone tags, blocked flips).
func (s *Sync) MakePlan() error {
	s.Log("ct sync: " + s.Source + ": listing open tickets...")
	remote, err := s.search(s.Query, []string{"updated", "status"})
	if err != nil {
		return err
	}
	locals := s.localIndex()
	notes := map[string]local{}
	for _, l := range locals {
		notes[l.id] = l
	}
	open := map[string]issue{}
	for _, r := range remote {
		open[r.Key] = r
	}

	// Open notes Jira's query didn't return: look them up by key
	var leavers []string
	for _, l := range locals {
		if _, ok := open[l.id]; l.status != "closed" && !ok {
			leavers = append(leavers, l.id)
		}
	}
	left := map[string]issue{}
	if len(leavers) > 0 {
		s.Log(fmt.Sprintf("ct sync: %s: checking %d ticket(s) no longer in your open list...", s.Source, len(leavers)))
		got, err := s.byKeys([]string{"updated", "status", "assignee"}, leavers)
		if err != nil {
			return err
		}
		for _, g := range got {
			left[g.Key] = g
		}
	}

	// Epics' children: refresh an epic when they (who, which status) changed
	var epics []string
	for _, l := range locals {
		if l.typ == "epic" && l.status != "closed" {
			epics = append(epics, l.id)
		}
	}
	if len(epics) > 0 {
		if s.Children, err = s.children(epics); err != nil {
			return err
		}
	}
	sigs := childSigs(s.Children)

	// Blockers' status, for blocked on every open note
	seen := map[string]bool{}
	var blockers []string
	for _, l := range locals {
		if l.status == "closed" {
			continue
		}
		for _, b := range l.blockers {
			if !seen[b] {
				seen[b] = true
				blockers = append(blockers, b)
			}
		}
	}
	sort.Strings(blockers)
	bcat := map[string]string{}
	if len(blockers) > 0 {
		got, err := s.byKeys([]string{"status"}, blockers)
		if err != nil {
			return err
		}
		for _, g := range got {
			bcat[g.Key] = g.Fields.cat()
		}
	}

	p := Plan{Source: s.Source, CloudID: s.cloud, AccountID: s.me,
		Create: []string{}, Refresh: []Refresh{}, Close: []string{}, Gone: []string{}, Blocked: []Flip{},
		EpicChildren: map[string][]string{}}
	changed := 0
	for _, r := range remote {
		n, ok := notes[r.Key]
		switch {
		case !ok:
			p.Create = append(p.Create, r.Key)
		case n.updated != r.Fields.Updated || !n.hasBlockedBy:
			reason := "dependencies"
			if n.updated != r.Fields.Updated {
				reason = "changed"
			}
			p.Refresh = append(p.Refresh, Refresh{n.id, reason})
			changed++
		}
	}
	for _, l := range locals {
		if _, ok := open[l.id]; l.status == "closed" || ok {
			continue
		}
		j, ok := left[l.id]
		switch {
		case !ok:
			if !has(l.tags, "unassigned") {
				p.Gone = append(p.Gone, l.id)
			}
		case j.Fields.cat() == "done":
			p.Close = append(p.Close, l.id)
		}
	}
	for _, l := range locals {
		if _, ok := open[l.id]; l.status == "closed" || ok {
			continue
		}
		j, ok := left[l.id]
		if !ok || j.Fields.cat() == "done" {
			continue
		}
		moved := j.Fields.Updated != l.updated
		assignee := ""
		if j.Fields.Assignee != nil {
			assignee = j.Fields.Assignee.AccountID
		}
		switch {
		case has(l.tags, "parent-epic"):
			if moved {
				p.Refresh = append(p.Refresh, Refresh{l.id, "parent-epic"})
			}
		case assignee != s.me || j.Fields.Assignee == nil:
			if moved || !has(l.tags, "unassigned") {
				p.Refresh = append(p.Refresh, Refresh{l.id, "unassigned"})
			}
		default:
			if moved {
				p.Refresh = append(p.Refresh, Refresh{l.id, "changed"})
			}
		}
	}
	for _, e := range epics {
		if _, ok := p.EpicChildren[e]; !ok {
			p.EpicChildren[e] = append([]string{}, sigs[e]...)
		}
	}
	// No baseline yet (first planned run): the last full sync wrote the
	// children, so record them without refreshing every epic
	if s.KnownChildren != nil {
		var base []string
		for _, r := range p.Refresh {
			base = append(base, r.ID)
		}
		done := map[string]bool{}
		for _, e := range epics {
			if done[e] {
				continue
			}
			done[e] = true
			known, ok := s.KnownChildren[e]
			if (!ok || !sameList(known, p.EpicChildren[e])) && !has(base, e) {
				p.Refresh = append(p.Refresh, Refresh{e, "children"})
			}
		}
	}
	agent := append(append([]string{}, p.Create...), p.Close...)
	for _, r := range p.Refresh {
		agent = append(agent, r.ID)
	}
	for _, l := range locals {
		if l.status == "closed" || len(l.blockers) == 0 || has(agent, l.id) {
			continue
		}
		var cats []string
		for _, b := range l.blockers {
			if c, ok := bcat[b]; ok && c != "" {
				cats = append(cats, c)
			} else if st := notes[b].status; st == "done" || st == "closed" {
				cats = append(cats, "done")
			}
		}
		if len(cats) == 0 {
			continue
		}
		now := false
		for _, c := range cats {
			now = now || c != "done"
		}
		if (l.blocked == "true") != now {
			p.Blocked = append(p.Blocked, Flip{l.id, now})
		}
	}
	p.Counts.Open = len(remote)
	p.Counts.Skipped = len(remote) - len(p.Create) - changed
	s.Plan = p

	// Written here: tags for vanished tickets, blocked flips
	for _, id := range p.Gone {
		f := s.notePath(id)
		_ = note.EditTags(f, true, "unassigned")
		_ = note.Set(f, "updated", s.today)
		s.Say(id + ": no longer found in Jira (deleted, moved, or no access): tagged unassigned")
		s.Followup("Check [[" + id + "]]: Jira no longer returns it (deleted, moved to another project, or access lost); close or delete the note")
	}
	for _, b := range p.Blocked {
		f := s.notePath(b.ID)
		_ = note.Set(f, "blocked", fmt.Sprint(b.Blocked))
		_ = note.Set(f, "updated", s.today)
		s.Say(fmt.Sprintf("%s: blocked: %t (blockers' status changed)", b.ID, b.Blocked))
	}
	return nil
}
