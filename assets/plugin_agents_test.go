package assets

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// frontmatter returns the YAML frontmatter of a markdown file as raw lines.
func frontmatter(t *testing.T, data string) []string {
	t.Helper()
	if !strings.HasPrefix(data, "---\n") {
		t.Fatal("no frontmatter")
	}
	end := strings.Index(data[4:], "\n---\n")
	if end < 0 {
		t.Fatal("unterminated frontmatter")
	}
	return strings.Split(data[4:4+end], "\n")
}

// field returns a scalar frontmatter field, or the items of a block list.
func field(lines []string, key string) (string, []string, bool) {
	for i, l := range lines {
		if !strings.HasPrefix(l, key+":") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(l, key+":"))
		var items []string
		for _, n := range lines[i+1:] {
			if !strings.HasPrefix(n, "  - ") {
				break
			}
			items = append(items, strings.TrimSpace(strings.TrimPrefix(n, "  - ")))
		}
		return v, items, true
	}
	return "", nil, false
}

// The wave workflow's agents (MAN-16 AC1, and the PO/lead split): the role
// agents are gone; each agent lists its tools explicitly without Agent;
// the workers pin a model and preload a brief skill the plugin ships. The
// lead inherits the session's model, can message but not spawn or ask the
// user, and reads the graph; the analyst is read-only.
func TestPluginAgents(t *testing.T) {
	for _, gone := range []string{"product-owner", "architect", "developer", "qa"} {
		if _, err := fs.Stat(Files, "plugin/agents/"+gone+".md"); err == nil {
			t.Errorf("role agent %s.md still ships", gone)
		}
	}
	want := map[string]bool{"task": true, "mechanical": true, "reviewer": true, "reviewer-deep": true, "lead": true, "analyst": true, "spec-writer": true}
	entries, err := fs.ReadDir(Files, "plugin/agents")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".md")
		if !want[name] {
			t.Errorf("unexpected agent %s", e.Name())
			continue
		}
		delete(want, name)
		data, err := fs.ReadFile(Files, "plugin/agents/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		fm := frontmatter(t, string(data))
		if v, _, _ := field(fm, "name"); v != name {
			t.Errorf("%s: name %q", e.Name(), v)
		}
		model, _, _ := field(fm, "model")
		if name == "lead" {
			if model != "inherit" {
				t.Errorf("lead.md: model %q, want inherit (the session default)", model)
			}
		} else if model == "" || model == "inherit" {
			t.Errorf("%s: no fixed model", e.Name())
		}
		tools, _, ok := field(fm, "tools")
		if !ok || tools == "" {
			t.Errorf("%s: no explicit tools list", e.Name())
		}
		has := map[string]bool{}
		for _, tool := range strings.Split(tools, ",") {
			tool = strings.TrimSpace(tool)
			has[tool] = true
			graph := strings.HasPrefix(tool, "mcp__graphify__")
			if tool == "Agent" || tool == "Task" || tool == "AskUserQuestion" || strings.HasPrefix(tool, "mcp__atlassian__") ||
				(graph && name != "lead" && name != "analyst") {
				t.Errorf("%s: tool %s isn't allowed", e.Name(), tool)
			}
		}
		switch name {
		case "lead":
			for _, tool := range []string{"SendMessage", "Skill", "Edit", "Bash", "mcp__graphify__query_graph", "mcp__fff__grep"} {
				if !has[tool] {
					t.Errorf("lead.md: no %s tool", tool)
				}
			}
		case "analyst":
			for _, tool := range []string{"Edit", "Write", "NotebookEdit", "SendMessage"} {
				if has[tool] {
					t.Errorf("analyst.md: read-only, but has %s", tool)
				}
			}
			if !has["mcp__graphify__query_graph"] || !has["mcp__fff__grep"] {
				t.Error("analyst.md: needs the graphify and fff tools")
			}
		case "spec-writer":
			for _, tool := range []string{"Edit", "NotebookEdit", "SendMessage"} {
				if has[tool] {
					t.Errorf("spec-writer.md: writes one file only, but has %s", tool)
				}
			}
			if !has["Write"] || !has["Read"] {
				t.Error("spec-writer.md: needs Read and Write")
			}
		case "task", "mechanical":
			if !has["SendMessage"] {
				t.Errorf("%s: no SendMessage for blocking questions to the lead", e.Name())
			}
		}
		_, skills, _ := field(fm, "skills")
		if len(skills) == 0 && name != "analyst" && name != "spec-writer" {
			t.Errorf("%s: preloads no brief skill", e.Name())
		}
		for _, s := range skills {
			skill := strings.TrimPrefix(s, "tickets:")
			if _, err := fs.Stat(Files, "plugin/skills/"+skill+"/SKILL.md"); err != nil {
				t.Errorf("%s: skill %s isn't in the plugin", e.Name(), s)
			}
			if !strings.Contains(string(data), "`"+s+"`") {
				t.Errorf("%s: no Skill-tool fallback naming %s", e.Name(), s)
			}
		}
	}
	for name := range want {
		t.Errorf("agent %s.md is missing", name)
	}
}

// The briefs and the lead's playbook (the tickets:lead skill) agree with the
// ticket-folder contract: review caps and path (AC6), the files the lead and
// the PO read on demand, and the templates the lead names.
func TestWaveSkillsReferences(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	review := read("plugin/skills/wave-review/SKILL.md")
	for _, s := range []string{"3 must-fix, 6 should-fix, 5 consider", "tickets/<source>/<ID>/reviews/wave-<N>-<slug>.md"} {
		if !strings.Contains(review, s) {
			t.Errorf("wave-review: missing %q", s)
		}
	}
	lead := read("plugin/skills/lead/SKILL.md")
	for _, df := range [][2]string{{"work-ticket", "kickoff.md"}, {"work-ticket", "handover.md"}, {"lead", "handover.md"}} {
		dir, f := df[0], df[1]
		if !strings.Contains(read("plugin/skills/"+dir+"/SKILL.md"), "`"+f+"`") {
			t.Errorf("%s doesn't point at %s", dir, f)
		}
		read("plugin/skills/" + dir + "/" + f)
	}
	for _, s := range []string{"4 agents in flight", "`owns` overlap never share a wave", "--no-ff", "≤3 calls"} {
		if !strings.Contains(lead, s) {
			t.Errorf("lead: missing %q", s)
		}
	}
	for _, tpl := range []string{"plan", "board", "task", "review", "backlog"} {
		if !strings.Contains(lead, "`"+tpl+"`") {
			t.Errorf("lead doesn't name template %s", tpl)
		}
		if _, err := fs.Stat(Scaffold, "vault-scaffold/templates/"+tpl+".md"); err != nil {
			t.Errorf("template %s.md isn't shipped", tpl)
		}
	}
	for _, tpl := range []string{"plan", "board", "task", "review", "backlog"} {
		data, _ := fs.ReadFile(Scaffold, "vault-scaffold/templates/"+tpl+".md")
		// Only board, review and backlog carry checklist lines (vault-checklists-v1).
		checkable := tpl == "board" || tpl == "review" || tpl == "backlog"
		for _, line := range strings.Split(string(data), "\n") {
			if !checkable && strings.HasPrefix(strings.TrimSpace(line), "- [") {
				t.Errorf("template %s.md has a checkbox: %q", tpl, line)
			}
		}
		if !strings.Contains(string(data), "type: "+tpl+"\n") {
			t.Errorf("template %s.md: no type: %s", tpl, tpl)
		}
	}
	// QA D1: a check's verdict is the command's own exit code, not that of
	// a `| grep | head` pipe after it.
	for _, p := range []string{"lead", "task-agent"} {
		s := read("plugin/skills/" + p + "/SKILL.md")
		if strings.Contains(s, "exit=$?") || !strings.Contains(s, "rc=$?") || !strings.Contains(s, "echo exit=$rc") {
			t.Errorf("%s: checks must report the command's exit code (rc=$? ... echo exit=$rc)", p)
		}
	}
	// QA D2, D7: git doesn't expand globs in revisions; a merge reverts with -m 1.
	if strings.Contains(lead, "..task/<ID>-NN-*") || !strings.Contains(lead, "git revert -m 1 <merge-sha>") {
		t.Error("lead: task-branch glob in a revision, or merge revert without -m 1")
	}
	// QA D6: save reads a review's ## Summary, which the review template has.
	if !strings.Contains(read("plugin/skills/save/SKILL.md"), "`## Summary`") {
		t.Error("save: should read each review's ## Summary section")
	}
	if data, _ := fs.ReadFile(Scaffold, "vault-scaffold/templates/review.md"); !strings.Contains(string(data), "\n## Summary\n") {
		t.Error("review template has no ## Summary section")
	}
}

// The PO/lead split (MAN-16): /tickets:work-ticket is the PO, which spawns
// one tickets:lead and every agent it asks for, relays through a capped
// relay.md, and points at /tickets:po-refresh; the lead never asks the user
// itself and ends each run with the final block the PO acts on.
func TestPOAndLeadSkills(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	lines := func(s string) int { return strings.Count(s, "\n") }

	po := read("plugin/skills/work-ticket/SKILL.md")
	if n := lines(po); n > 135 {
		t.Errorf("work-ticket (PO) is %d lines, cap 135", n)
	}
	for _, s := range []string{
		"`tickets:lead`", "`tickets:analyst`", "`relay.md`", "/tickets:po-refresh", "≤3 calls",
		"`lead:`", "`po:`", "AskUserQuestion", "Lead: <agentId>",
		"`wait-tasks`", "`wait-user`", "`rotate`", "`handover-done`", "never two at once",
		"**A compaction is always the same session**",
	} {
		if !strings.Contains(po, s) {
			t.Errorf("work-ticket (PO): missing %q", s)
		}
	}
	// relay caps (moved to workflow.md "relay.md format")
	poFull := po + read("plugin/skills/work-ticket/workflow.md")
	for _, s := range []string{"`## Open` holds **≤30** entries", "`## Closed` **≤40**", "about 100 lines"} {
		if !strings.Contains(poFull, s) {
			t.Errorf("work-ticket (PO) and workflow.md: missing %q", s)
		}
	}

	leadSkill := read("plugin/skills/lead/SKILL.md")
	if n := lines(leadSkill); n > 233 {
		t.Errorf("lead/SKILL.md is %d lines, cap 233", n)
	}
	if n := lines(read("plugin/skills/lead/reference.md")); n > 75 {
		t.Errorf("lead/reference.md is %d lines, cap 75", n)
	}
	lead := leadSkill + read("plugin/skills/lead/handover.md") + read("plugin/agents/lead.md")
	if strings.Contains(lead, "AskUserQuestion") {
		t.Error("the lead must not ask the user directly (AskUserQuestion)")
	}
	for _, s := range []string{"Spawn:", "Ask user:", "Status:", "Next: wait-tasks | wait-user | rotate | handover-done",
		"relay.md", "## Open", "SendMessage", "`## Open` ≤30", "`## Closed` ≤40"} {
		if !strings.Contains(lead, s) {
			t.Errorf("lead: missing %q", s)
		}
	}
	if !strings.Contains(read("plugin/skills/task-agent/SKILL.md"), "Lead: <agentId>") {
		t.Error("task-agent: no optional Lead: <agentId> line for blocking questions")
	}
	if !strings.Contains(read("plugin/skills/wave-review/SKILL.md"), "your spawner, the PO") {
		t.Error("wave-review: the reply goes to the spawner, the PO")
	}

	refresh := read("plugin/skills/po-refresh/SKILL.md")
	if n := lines(refresh); n > 50 {
		t.Errorf("po-refresh is %d lines, cap 50", n)
	}
	for _, s := range []string{"`board.md`", "`lead:", "`po:`", "`relay.md", "/compact Keep only: ticket <ID>"} {
		if !strings.Contains(refresh, s) {
			t.Errorf("po-refresh: missing %q", s)
		}
	}
	// /clear may orphan the lead and the workers: every line that offers or
	// mentions it carries the guard (no active lead, no running row).
	for _, l := range strings.Split(refresh, "\n") {
		if strings.Contains(l, "/clear") && l != "/clear" && !strings.Contains(l, "running") {
			t.Errorf("po-refresh: /clear without the running/lead guard: %q", l)
		}
	}
	if !strings.Contains(refresh, "only when `lead:` is `none` and no board row is `running`") {
		t.Error("po-refresh: /clear guard missing")
	}
	// it picks by itself, or takes clear|compact; an unsafe clear waits or compacts
	for _, s := range []string{"/tickets:po-refresh [clear|compact]", "**Wait**", "**Compact now**", "Safe now:",
		"/clear\n/tickets:work-ticket <ID>\n", "**on your own**", "`autoCompactWindow`"} {
		if !strings.Contains(refresh, s) {
			t.Errorf("po-refresh: missing %q", s)
		}
	}
}

// The ticket tidy skill: model-invocable, at most 100 lines.
func TestTidySkillFrontmatter(t *testing.T) {
	for _, name := range []string{"tidy"} {
		data, err := fs.ReadFile(Files, "plugin/skills/"+name+"/SKILL.md")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		s := string(data)
		fm := frontmatter(t, s)
		if v, _, _ := field(fm, "name"); v != name {
			t.Errorf("%s: name %q", name, v)
		}
		if v, _, _ := field(fm, "description"); len(v) < 40 {
			t.Errorf("%s: description too short: %q", name, v)
		}
		if v, _, ok := field(fm, "disable-model-invocation"); ok && v == "true" {
			t.Errorf("%s: must be model-invocable", name)
		}
		if n := strings.Count(s, "\n"); n > 100 {
			t.Errorf("%s is %d lines, cap 100", name, n)
		}
	}
}

// The skills adapted from mattpocock/skills (MIT; MAN-16 addendum): each
// ships with its name, a description, the attribution footer and a size
// cap; retro and teach are user-invoked only; the notices file
// carries the license; the PO, the lead and the task brief hook into them.
func TestAdaptedSkills(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	const footer = "Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md"
	notices := read("plugin/THIRD_PARTY_NOTICES.md")
	for _, s := range []string{"MIT License", "Copyright (c) 2026 Matt Pocock",
		"THE SOFTWARE IS PROVIDED \"AS IS\"", "mattpocock/skills"} {
		if !strings.Contains(notices, s) {
			t.Errorf("THIRD_PARTY_NOTICES.md: missing %q", s)
		}
	}
	for _, sk := range []struct {
		name        string
		cap         int
		userInvoked bool
	}{
		{"clarify", 90, false}, {"diagnose", 90, false},
		{"retro", 45, true}, {"teach", 120, true},
	} {
		s := read("plugin/skills/" + sk.name + "/SKILL.md")
		fm := frontmatter(t, s)
		if v, _, _ := field(fm, "name"); v != sk.name {
			t.Errorf("%s: name %q", sk.name, v)
		}
		if v, _, _ := field(fm, "description"); len(v) < 40 {
			t.Errorf("%s: description too short: %q", sk.name, v)
		}
		v, _, ok := field(fm, "disable-model-invocation")
		if sk.userInvoked != (ok && v == "true") {
			t.Errorf("%s: disable-model-invocation %q, want user-invoked=%v", sk.name, v, sk.userInvoked)
		}
		if n := strings.Count(s, "\n"); n > sk.cap {
			t.Errorf("%s is %d lines, cap %d", sk.name, n, sk.cap)
		}
		if !strings.Contains(s, footer) {
			t.Errorf("%s: no attribution footer", sk.name)
		}
		if !strings.Contains(notices, "`skills/"+sk.name+"`") {
			t.Errorf("THIRD_PARTY_NOTICES.md doesn't list skills/%s", sk.name)
		}
	}

	clarify := read("plugin/skills/clarify/SKILL.md")
	for _, s := range []string{"AskUserQuestion", "**≤4 questions**", "(Recommended)", "`tickets:analyst`",
		"frontier", "skip", "--from user --to lead", "## Questions", "Brief:", "[NEEDS CLARIFICATION]",
		"/tickets:clarify questionnaire <who>", "tickets/<source>/<ID>/<ID>-questionnaire-<slug>.md"} {
		if !strings.Contains(clarify, s) {
			t.Errorf("clarify: missing %q", s)
		}
	}
	for _, gone := range []string{"kb", "grill", "grilling", "questionnaire", "docs"} {
		if _, err := fs.ReadFile(Files, "plugin/skills/"+gone+"/SKILL.md"); err == nil {
			t.Errorf("skill %s was renamed or removed", gone)
		}
	}
	po := read("plugin/skills/work-ticket/SKILL.md") + read("plugin/skills/work-ticket/kickoff.md")
	for _, s := range []string{"`tickets:clarify`", "/tickets:clarify questionnaire", "<ID>-questionnaire-<slug>.md", "#follow-up"} {
		if !strings.Contains(po, s) {
			t.Errorf("PO (work-ticket, kickoff): missing %q", s)
		}
	}

	diagnose := read("plugin/skills/diagnose/SKILL.md")
	for _, s := range []string{"## 1. Build a feedback loop", "## 2. Reproduce", "## 3. Hypothesise",
		"## 4. Fix", "## 5. Regression test", "<REDACTED>", "skills: [tickets:diagnose]"} {
		if !strings.Contains(diagnose, s) {
			t.Errorf("diagnose: missing %q", s)
		}
	}
	lead := read("plugin/skills/lead/SKILL.md") + read("plugin/skills/lead/reference.md")
	for _, s := range []string{"`tickets:diagnose`", "build the feedback loop", "`plan.md ## Root cause`", "`tickets:clarify`"} {
		if !strings.Contains(lead, s) {
			t.Errorf("lead: missing %q", s)
		}
	}
	if !strings.Contains(read("plugin/skills/task-agent/SKILL.md"), "skills: [tickets:diagnose]") {
		t.Error("task-agent: doesn't load the skills its task file lists")
	}
	handover := read("plugin/skills/lead/handover.md")
	for _, s := range []string{"`/tickets:retro`", "**at most 8**", "retro: <change>"} {
		if !strings.Contains(handover, s) {
			t.Errorf("handover: missing %q", s)
		}
	}

	teach := read("plugin/skills/teach/SKILL.md")
	for _, s := range []string{
		"`learning/<topic>/`", "`<topic>.md`", "`<topic>-resources.md`", "`<topic>-glossary.md`",
		"`<topic>-notes.md`", "`lessons/<topic>-NNNN-<name>.md`", "`reference/<topic>-<name>.md`",
		"`records/<topic>-NNNN-<name>.md`", "`quizzes/<topic>-NNNN-<name>.html`",
		"`learning-topic`", "`lesson`", "`learning-record`", "`learning-reference`",
		"ct vault lock acquire <vault> teach-<topic>", "**WebSearch**", "**WebFetch**",
		"zone of proximal development", "the same number of words", "## Wisdom",
	} {
		if !strings.Contains(teach, s) {
			t.Errorf("teach: missing %q", s)
		}
	}
}

// The scaffold carries what the PO, the lead and the teach skill rely on:
// the relay template (Closed, then Open), the PO's board keys, the task's
// skills list, the learning templates' fields, and AGENTS.md's account of them.
func TestScaffoldForPOAndTeach(t *testing.T) {
	tpl := func(name string) string {
		data, err := fs.ReadFile(Scaffold, "vault-scaffold/templates/"+name+".md")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	relay := tpl("relay")
	closed, open := strings.Index(relay, "\n## Closed\n"), strings.Index(relay, "\n## Open\n")
	if closed < 0 || open < closed {
		t.Error("relay template: ## Closed must come before ## Open (the PO tails Open)")
	}
	for _, s := range []string{"type: relay\n", `ticket: "[[TICKET-ID]]"`, "≤30", "≤40",
		"<request|feature|feedback|question|answer|decision>", "<open|planned|answered|copied>", "`copied`"} {
		if !strings.Contains(relay, s) {
			t.Errorf("relay template: missing %q", s)
		}
	}
	if strings.Contains(relay, "- [ ]") {
		t.Error("relay template has a checkbox")
	}
	for _, s := range []string{"\nlead: none\n", "\nlead-since: \"\"\n", "\npo:\n  session: \"\"\n  last-refresh: \"\"\n  exchanges-since: 0\n  analyst-since: 0\n  agents: []\n"} {
		if !strings.Contains(tpl("board"), s) {
			t.Errorf("board template: missing %q", s)
		}
	}
	if !strings.Contains(tpl("task"), "\nskills: []\n") {
		t.Error("task template: no skills: []")
	}
	for name, want := range map[string][]string{
		"learning-topic":     {"type: learning-topic\n", "## Mission", "## Lessons", "## Reference", "## Records"},
		"lesson":             {"type: lesson\n", "number: ", "source:", "quiz:", "## Win", "## Lesson", "## Practice", "## Primary source"},
		"learning-record":    {"type: learning-record\n", "number: ", "status: active\n", "superseded-by: \"\"\n", "## Evidence", "## Implications"},
		"learning-reference": {"type: learning-reference\n", "sources: []\n"},
	} {
		text := tpl(name)
		for _, s := range append(want, "projects: []\n", "topic: \"[[", "tags: [learning]") {
			if !strings.Contains(text, s) {
				t.Errorf("%s template: missing %q", name, s)
			}
		}
	}
	for _, sk := range []string{"spec", "recall", "save", "teach", "work-ticket", "lead", "task-agent", "wave-review"} {
		b, err := fs.ReadFile(Files, "plugin/skills/"+sk+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "ct vault search") {
			t.Errorf("skill %s does not mention ct vault search", sk)
		}
	}
	data, _ := fs.ReadFile(Scaffold, "vault-scaffold/AGENTS.md")
	agents := string(data)
	for _, s := range []string{"relay.md", "`lead:`, `lead-since:` and `po:`", "### Learning topics", "ct teach",
		"`learning/<topic>/`", "teach-<topic>`)", "questionnaire"} {
		if !strings.Contains(agents, s) {
			t.Errorf("vault AGENTS.md: missing %q", s)
		}
	}
	// The workflow prose moved out of AGENTS.md into the skills (task 10).
	workflow, _ := fs.ReadFile(Files, "plugin/skills/work-ticket/workflow.md")
	for _, s := range []string{"`tickets:lead`", "`tickets:analyst`", "`tickets:clarify`", "`tickets:diagnose`",
		"/tickets:po-refresh", "`/tickets:retro`", "`lead:` and\n`po:`"} {
		if !strings.Contains(string(workflow), s) {
			t.Errorf("work-ticket/workflow.md: missing %q", s)
		}
	}
	// AGENTS.md is a short rulebook: at most 215 lines, the headings code cites kept.
	if n := strings.Count(agents, "\n"); n > 215 {
		t.Errorf("vault AGENTS.md has %d lines, want at most 215", n)
	}
	for _, h := range []string{"\n### Who writes what\n", "\n### Ticket sources\n", "\n### Task lists\n", "\n## Note Rules\n"} {
		if !strings.Contains(agents, h) {
			t.Errorf("vault AGENTS.md: heading %q missing", strings.TrimSpace(h))
		}
	}
}

// No line inside a template's HTML comment starts with `- [`: the Tasks plugin
// would list it as a task (vault-checklists-v1). Examples go in backticks.
func TestTemplateCommentsHaveNoCheckboxLines(t *testing.T) {
	err := fs.WalkDir(Scaffold, "vault-scaffold/templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(Scaffold, p)
		if err != nil {
			return err
		}
		inComment := false
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "<!--") {
				inComment = true
			}
			if inComment && strings.HasPrefix(strings.TrimSpace(line), "- [") {
				t.Errorf("%s:%d: comment line starts with a checkbox: %q", p, i+1, line)
			}
			if strings.Contains(line, "-->") {
				inComment = false
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Plugin skill rules: the lead's merge chain and ancestor-based branch
// deletion, the reviewer's capped focused tests, the task agent's report
// heading and early commits, and the formats note in workflow.md.
func TestPluginSkillRules(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
	lead := read("plugin/skills/lead/SKILL.md")
	review := read("plugin/skills/wave-review/SKILL.md")
	task := read("plugin/skills/task-agent/SKILL.md")
	wf := read("plugin/skills/work-ticket/workflow.md")
	for name, c := range map[string]struct {
		text string
		want []string
	}{
		"lead": {lead, []string{
			"merge `&&` checks `&&` board line", "can never leave a merged task `running`",
			"Ancestry is no proof", "index($3,t)==1||index(t,$3)==1", "-m \"<subject>\"", "test `orphaned` lines only", "recorded, not re-merged",
			"`git revert -m 1 <merge-sha>` it (never reset) and mark the line `partial`", "leaves no reflog and any `branch.<name>.*` config",
			"git merge-base --is-ancestor <b> HEAD && git update-ref -d refs/heads/<b>",
		}},
		"wave-review": {review, []string{"timeout 300", "focused tests of the changed packages", "`confirmed`", "`unconfirmed`", "**3 must-fix, 6 should-fix, 5 consider**", "`regression of <earlier ID>`", "`pre-existing`"}},
		"task-agent":  {task, []string{"existing `## Report` heading", "the line that is exactly `## Report`", "never append a second", "Commit early", "read `git diff` before committing", "git status --short", "build outputs", "never `git add -A` blindly", "Never hand back while a command you started in the background still runs", "frontmatter `id:` is your task number"}},
		"workflow":    {wf, []string{"A ticket that changes the plugin's own formats", "in the format of the plugin version"}},
	} {
		for _, s := range c.want {
			if !strings.Contains(c.text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}
	if strings.Contains(lead, "(`git branch -d`)") || strings.Contains(lead, "&& git branch -d") {
		t.Error("lead: still deletes task branches with git branch -d")
	}
	if strings.Contains(task, "Append it to your task file") {
		t.Error("task-agent: must not append a second ## Report")
	}
}

// The lead's SKILL.md points at each part moved to reference.md by heading,
// and the reference file has that heading, so no pointer dangles.
func TestLeadRefPointers(t *testing.T) {
	skill, err := Files.ReadFile("plugin/skills/lead/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := Files.ReadFile("plugin/skills/lead/reference.md")
	if err != nil {
		t.Fatal(err)
	}
	handover, err := Files.ReadFile("plugin/skills/lead/handover.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(handover), "`reference.md`, \"Gate script\"") {
		t.Error("lead/handover.md: no pointer to reference.md \"Gate script\"")
	}
	for _, h := range []string{"Board line", "Disposition line formats", "Gate script", "Ticket types"} {
		if !strings.Contains(string(skill), "`reference.md`, \""+h+"\"") {
			t.Errorf("lead/SKILL.md: no pointer to reference.md %q", h)
		}
		if !strings.Contains(string(ref), "\n## "+h) {
			t.Errorf("lead/reference.md: no heading %q", h)
		}
	}
}

// The spec skill's planning and analyze flows and the new-ticket epic flow.
func TestSpecPlanningFlows(t *testing.T) {
	read := func(name string) string {
		b, err := fs.ReadFile(Files, name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	spec := read("plugin/skills/spec/SKILL.md")
	for _, s := range []string{"## Planning a change", "## Analyze", "`tickets:clarify`", "`ct spec location <slug>`",
		"`## Spec change`", "[NEEDS CLARIFICATION", "blocks filing", "`spec-draft.md`", "`ct spec check", "--json",
		"`/tickets:new-ticket`", "copy in the ticket folder", "`/tickets:save` promotes", "Never edit a spec in analyze"} {
		if !strings.Contains(spec, s) {
			t.Errorf("spec skill: missing %q", s)
		}
	}
	nt := read("plugin/skills/new-ticket/SKILL.md")
	epic := read("plugin/skills/new-ticket/epic.md")
	if !strings.Contains(nt, "`epic.md`") {
		t.Error("new-ticket skill does not point to epic.md")
	}
	for _, s := range []string{"Epic", "`## Spec change`", "[NEEDS CLARIFICATION", "blocks filing", "`spec-draft.md`", "parent", "first"} {
		if !strings.Contains(epic, s) {
			t.Errorf("new-ticket epic flow: missing %q", s)
		}
	}
}

// The lead, task agent and reviewer sides of the spec workflow and the retro
// rules: ready specs, requirement IDs, review against ct spec diff, relay
// entries only through ct relay, quoted heredocs, fixture sync at merge, and
// the task agent's report discipline.
func TestSpecWorkflowLeadSide(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	lead := read("plugin/skills/lead/SKILL.md")
	specs := read("plugin/skills/lead/specs.md")
	has := func(name, text string, want ...string) {
		for _, s := range want {
			if !strings.Contains(text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}
	has("lead/SKILL.md", lead, "`specs.md`", "ct relay add <ID>", "ct relay state <ID>", "never `>>` or `sed -i` on `relay.md`",
		"<<'EOF'", "never an unquoted heredoc", "fixture sync", "goldens or fixtures another repo copies")
	has("lead/specs.md", specs, "ct spec diff", "## Specs", "`ready`", "`requirements:`", "`## Interfaces`",
		"only shapes private to this ticket's tasks", "never edit `specs/`", "`implemented`", "waits until that one is `merged`")
	has("lead/handover.md", read("plugin/skills/lead/handover.md"), "## Specs", "`specs.md`")
	has("agents/lead.md", read("plugin/agents/lead.md"), "ready specs")
	task := read("plugin/skills/task-agent/SKILL.md")
	has("task-agent", task, "`requirements:`", "cites each ID", "never edits `specs/`", "sed -n '/^## Report/,$p'",
		"never hand back without it", "never a shell redirect that rewrites the task file")
	has("wave-review", read("plugin/skills/wave-review/SKILL.md"), "`ct spec diff <ID>`", "`proposed` requirement",
		"`implemented` one", "the PO flips them to `implemented`", "ticket-private shapes only")
	if strings.Contains(lead, "`relay.md`: `R-nn") || strings.Contains(lead, "rg -o 'R-[0-9]+' relay.md") {
		t.Error("lead/SKILL.md still allocates relay ids by hand")
	}
	has("AGENTS.md", read2(t, "../AGENTS.md"), "Specs over contracts", "`requirements:`")
}

func read2(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The spec-init skill: the gate, the draft paths, the writer subagent and the
// architecture skeleton it takes over from the removed docs skill.
func TestSpecInitSkill(t *testing.T) {
	read := func(p string) string {
		b, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	skill := read("plugin/skills/spec-init/SKILL.md")
	if n := strings.Count(skill, "\n"); n > 100 {
		t.Errorf("spec-init: %d lines, cap 100", n)
	}
	for _, f := range []string{"name: spec-init\n", "description: ", "argument-hint: "} {
		if !strings.Contains(skill, f) {
			t.Errorf("spec-init: frontmatter lacks %q", f)
		}
	}
	for _, s := range []string{"ct vault search", "get_community", "## 2. Gate", "Draft nothing before the approval",
		"ct spec location", "/tickets:new-ticket", "tickets:spec-writer", "one message",
		"tickets/<source>/<ID>/specs/<slug>/", "tickets/<source>/<ID>/spec-init/", "<capability>/spec.md",
		"status: inferred", "file:line", "NEEDS CLARIFICATION", "depends-on", "docs/architecture.md",
		"<out>README.md", "ct spec check --dir <out>", "tickets/<source>/<ID>/docs-architecture.md", "Shallow on purpose", "never a commit"} {
		if !strings.Contains(skill, s) {
			t.Errorf("spec-init: missing %q", s)
		}
	}
	w := read("plugin/agents/spec-writer.md")
	for _, s := range []string{"status: inferred", "evidence:", "tests:", "at most 3", "depends-on", "file:line", "Write only the output path"} {
		if !strings.Contains(w, s) {
			t.Errorf("spec-writer: missing %q", s)
		}
	}
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "`/tickets:spec-init`") {
		t.Error("README skill table doesn't list /tickets:spec-init")
	}
}

// Save publishes the spec change and offers transitions; new-ticket stubs a
// child filed under the lead.
func TestSaveAndNewTicketSpecSide(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
	save := read("plugin/skills/save/SKILL.md")
	for _, s := range []string{"`ct ticket publish <ID> --dry-run --description-file <f>`", "`ct ticket publish <ID> --description-file <f>`", "`ct spec promote <ID> --json`", "Check the JSON's `error` (or a non-zero exit) first", "when `others` is empty, say `projects/` was edited by hand or by an unknown ticket", "--resolve <slug>=theirs", "one question per clashing repo", "keep theirs, keep ours, merge by hand", "`ct spec promote <ID> --resolve <slug>=merged`", "`ct spec pin <ID>`", "`editJiraIssue`", "`getTransitionsForJiraIssue`",
		"`transitionJiraIssue`", "ask **once** with AskUserQuestion", "`ct spec diff <ID>`", "`specs:` and `requirements:`",
		"`ct vault links move`", "has no remote description", "are not reachable in this session"} {
		if !strings.Contains(save, s) {
			t.Errorf("save: missing %q", s)
		}
	}
	if strings.Contains(save, "ct spec baseline") {
		t.Error("save: names the removed ct spec baseline")
	}
	nt := read("plugin/skills/new-ticket/SKILL.md")
	for _, s := range []string{"`ct ticket stub <NEW-ID> --source <source> --summary \"<title>\" --covered-by <lead>`",
		"don't write that note by hand", "The PO tells the lead"} {
		if !strings.Contains(nt, s) {
			t.Errorf("new-ticket: missing %q", s)
		}
	}
}

// Skills add relay entries only through ct relay add, never by hand.
func TestSkillsNeverAppendRelayByHand(t *testing.T) {
	err := fs.WalkDir(Files, "plugin/skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			return err
		}
		text := strings.Join(strings.Fields(string(data)), " ")
		for _, bad := range []string{"append `relay.md` line", "append one `relay.md` entry", "append a `relay.md` entry"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s: %q (use ct relay add)", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"clarify", "retro", "tidy"} {
		data, err := fs.ReadFile(Files, "plugin/skills/"+p+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "ct relay add") {
			t.Errorf("%s: no ct relay add", p)
		}
	}
	data, _ := fs.ReadFile(Files, "plugin/skills/clarify/SKILL.md")
	text := strings.Join(strings.Fields(string(data)), " ")
	for _, s := range []string{"at most 200 characters", "plan.md` under `## Scope`", "a pointer"} {
		if !strings.Contains(text, s) {
			t.Errorf("clarify: missing %q", s)
		}
	}
}

// Every skill names a capability spec as specs/<capability>/spec.md.
func TestSkillsSpecPath(t *testing.T) {
	err := fs.WalkDir(Files, "plugin/skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "specs/<capability>.md") {
			t.Errorf("%s: names specs/<capability>.md, want specs/<capability>/spec.md", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := fs.ReadFile(Files, "plugin/skills/lead/specs.md")
	if !strings.Contains(string(data), "specs/<capability>/spec.md") {
		t.Error("lead/specs.md: lacks specs/<capability>/spec.md")
	}
}

// Save's order: specs promoted before the link check, publish and transitions
// after the push, per ticket, and the lock released before any question.
func TestSaveStepOrder(t *testing.T) {
	data, err := fs.ReadFile(Files, "plugin/skills/save/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	idx := func(s string) int {
		i := strings.Index(text, s)
		if i < 0 {
			t.Fatalf("save: missing %q", s)
		}
		return i
	}
	promote := idx("`ct spec promote <ID> --json`")
	links := idx("### 5. Check links")
	push := idx("### 8. Push the ticket branches")
	publish := idx("### 9. Publish the descriptions and offer transitions")
	lock := idx("Run `ct vault lock release <vault> <ID>`")
	if !(promote < links && links < lock && lock < push && push < publish) {
		t.Errorf("save: step order wrong: promote %d links %d lock %d push %d publish %d", promote, links, lock, push, publish)
	}
	if pin := idx("`ct spec pin <ID>`"); pin < push || pin > publish {
		t.Errorf("save: ct spec pin must follow the push (step 8): pin %d push %d publish %d", pin, push, publish)
	}
	for _, s := range []string{"`getTransitionsForJiraIssue`", "`transitionJiraIssue`", "`editJiraIssue`", "ct ticket publish"} {
		if i := strings.Index(text, s); i < push {
			t.Errorf("save: %s appears before the push step", s)
		}
	}
	flat := strings.Join(strings.Fields(text), " ")
	for _, s := range []string{"Decide **per ticket**", "each ticket in its `covers`", "name it and skip it", "never the bare date",
		"`getJiraIssue` right before the edit", "never holds it"} {
		if !strings.Contains(flat, s) {
			t.Errorf("save: missing %q", s)
		}
	}
}

// New-ticket: the stub covers a synced child under a lead; a manual child is
// not stubbed and keeps the covers line.
func TestNewTicketStubWording(t *testing.T) {
	data, err := fs.ReadFile(Files, "plugin/skills/new-ticket/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	for _, bad := range []string{"is never yours to write before", "ct sync` writes a synced ticket's note at", "never write the note yourself"} {
		if strings.Contains(text, bad) {
			t.Errorf("new-ticket: stale %q", bad)
		}
	}
	for _, s := range []string{"never a manual child, which `ct ticket stub` refuses", "filed at a synced source",
		"don't add a follow-up \"add to `covers`\" line", "Under a manual lead, a manual child"} {
		if !strings.Contains(text, s) {
			t.Errorf("new-ticket: missing %q", s)
		}
	}
}

// Every skill that makes a ticket's copy of a vault spec makes it with ct spec
// start, which takes the snapshot promote compares against; save handles a
// copy made without it.
func TestSpecCopiesStartWithSnapshot(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(data)), " ")
	}
	for _, c := range []struct{ file, want string }{
		{"plugin/skills/spec/SKILL.md", "`ct spec start <ID> <slug> [<capability>…]`, never by hand"},
		{"plugin/skills/work-ticket/specs.md", "`ct spec start <ID> <slug> [<capability>…]`"},
		{"plugin/skills/work-ticket/specs.md", "never copy by hand"},
		{"plugin/skills/spec-init/SKILL.md", "make it first with `ct spec start <ID> <slug>`"},
		{"plugin/skills/save/SKILL.md", "it checks against the snapshot `ct spec start` took"},
		{"plugin/skills/save/SKILL.md", "names the `ct spec start <ID> <slug>` to run"},
		{"plugin/skills/save/SKILL.md", "against the snapshot `ct spec start` took"},
	} {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q", c.file, c.want)
		}
	}
	if save := read("plugin/skills/save/SKILL.md"); strings.Contains(save, "snapshots the base") || strings.Contains(save, "baseline step 4 recorded") {
		t.Error("save: still says promote takes the base snapshot")
	}
}
