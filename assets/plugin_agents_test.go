package assets

import (
	"io/fs"
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
	want := map[string]bool{"task": true, "mechanical": true, "reviewer": true, "reviewer-deep": true, "lead": true, "analyst": true}
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
		case "task", "mechanical":
			if !has["SendMessage"] {
				t.Errorf("%s: no SendMessage for blocking questions to the lead", e.Name())
			}
		}
		_, skills, _ := field(fm, "skills")
		if len(skills) == 0 && name != "analyst" {
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
	if n := lines(leadSkill); n > 228 {
		t.Errorf("lead/SKILL.md is %d lines, cap 228", n)
	}
	if n := lines(read("plugin/skills/lead/reference.md")); n > 60 {
		t.Errorf("lead/reference.md is %d lines, cap 60", n)
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
		if strings.Contains(l, "/clear") && !strings.Contains(l, "running") {
			t.Errorf("po-refresh: /clear without the running/lead guard: %q", l)
		}
	}
	if !strings.Contains(refresh, "only when `lead:` is `none` and no board row is `running`") {
		t.Error("po-refresh: /clear guard missing")
	}
}

// The ticket docs and tidy skills: model-invocable, at most 100 lines.
func TestDocsTidySkillFrontmatter(t *testing.T) {
	for _, name := range []string{"docs", "tidy"} {
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
// cap; grill, retro and teach are user-invoked only; the notices file
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
		{"grilling", 40, false}, {"grill", 30, true}, {"diagnose", 90, false},
		{"retro", 45, true}, {"questionnaire", 50, false}, {"teach", 120, true},
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

	grilling := read("plugin/skills/grilling/SKILL.md")
	for _, s := range []string{"AskUserQuestion", "**≤4 questions**", "(Recommended)", "`tickets:analyst`",
		"frontier", "skip", "user→lead · decision", "## Questions", "Brief:"} {
		if !strings.Contains(grilling, s) {
			t.Errorf("grilling: missing %q", s)
		}
	}
	if !strings.Contains(read("plugin/skills/grill/SKILL.md"), "`tickets:grilling`") {
		t.Error("grill: doesn't load tickets:grilling")
	}
	po := read("plugin/skills/work-ticket/SKILL.md") + read("plugin/skills/work-ticket/kickoff.md")
	for _, s := range []string{"`tickets:grilling`", "`tickets:questionnaire`", "<ID>-questionnaire-<slug>.md", "#follow-up"} {
		if !strings.Contains(po, s) {
			t.Errorf("PO (work-ticket, kickoff): missing %q", s)
		}
	}
	if !strings.Contains(read("plugin/skills/questionnaire/SKILL.md"), "tickets/<source>/<ID>/<ID>-questionnaire-<slug>.md") {
		t.Error("questionnaire: wrong file path")
	}

	diagnose := read("plugin/skills/diagnose/SKILL.md")
	for _, s := range []string{"## 1. Build a feedback loop", "## 2. Reproduce", "## 3. Hypothesise",
		"## 4. Fix", "## 5. Regression test", "<REDACTED>", "skills: [tickets:diagnose]"} {
		if !strings.Contains(diagnose, s) {
			t.Errorf("diagnose: missing %q", s)
		}
	}
	lead := read("plugin/skills/lead/SKILL.md") + read("plugin/skills/lead/reference.md")
	for _, s := range []string{"`tickets:diagnose`", "build the feedback loop", "`plan.md ## Root cause`", "`tickets:grilling`"} {
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
	for _, sk := range []string{"kb", "recall", "save", "teach", "work-ticket", "lead", "task-agent", "wave-review"} {
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
	for _, s := range []string{"`tickets:lead`", "`tickets:analyst`", "`tickets:grilling`", "`tickets:diagnose`",
		"/tickets:po-refresh", "`/tickets:retro`", "`lead:` and\n`po:`"} {
		if !strings.Contains(string(workflow), s) {
			t.Errorf("work-ticket/workflow.md: missing %q", s)
		}
	}
	// AGENTS.md is a short rulebook: at most 200 lines, the headings code cites kept.
	if n := strings.Count(agents, "\n"); n > 200 {
		t.Errorf("vault AGENTS.md has %d lines, want at most 200", n)
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
		"wave-review": {review, []string{"timeout 300", "focused tests of the changed packages", "`confirmed`", "`unconfirmed`", "**3 must-fix, 6 should-fix, 5 consider**"}},
		"task-agent":  {task, []string{"existing `## Report` heading", "the line that is exactly `## Report`", "never append a second", "Commit early", "read `git diff` before committing", "git status --short", "build outputs", "never `git add -A` blindly"}},
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
