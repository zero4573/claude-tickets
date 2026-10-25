package assets

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pluginTexts returns every Markdown file of the plugin's skills and agents.
func pluginTexts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(Files, "plugin", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") || strings.HasSuffix(p, "THIRD_PARTY_NOTICES.md") {
			return nil
		}
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			return err
		}
		out[p] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every tickets:<name> the skills and agents mention is an agent or a skill
// the plugin ships, so no spawn request or skill load names something gone
// (e.g. an old role agent or a renamed skill).
func TestPluginNamesResolve(t *testing.T) {
	known := map[string]bool{}
	for _, dir := range []string{"plugin/agents", "plugin/skills"} {
		entries, err := fs.ReadDir(Files, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			known[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	re := regexp.MustCompile(`tickets:([a-z][a-z-]*[a-z])`)
	for p, s := range pluginTexts(t) {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			if !known[m[1]] {
				t.Errorf("%s names tickets:%s, which the plugin doesn't ship", p, m[1])
			}
		}
	}
}

// A skill with disable-model-invocation: true can only be run by the user
// (/tickets:<name>). No other skill or agent may tell a model to load it as
// `tickets:<name>`; mentions of the slash command are fine.
func TestUserInvokedSkillsNotModelLoaded(t *testing.T) {
	texts := pluginTexts(t)
	var userOnly []string
	for p, s := range texts {
		if !strings.HasSuffix(p, "/SKILL.md") || !strings.Contains(s, "\ndisable-model-invocation: true\n") {
			continue
		}
		userOnly = append(userOnly, strings.TrimSuffix(strings.TrimPrefix(p, "plugin/skills/"), "/SKILL.md"))
	}
	if len(userOnly) < 3 {
		t.Fatalf("expected at least grill, retro and teach to be user-invoked, got %v", userOnly)
	}
	for _, name := range userOnly {
		re := regexp.MustCompile(`(^|[^/a-z])tickets:` + regexp.QuoteMeta(name) + `\b`)
		for p, s := range texts {
			if p == "plugin/skills/"+name+"/SKILL.md" {
				continue
			}
			if loc := re.FindStringIndex(s); loc != nil {
				t.Errorf("%s refers to the user-invoked skill as tickets:%s (a model can't load it); use /tickets:%s or inline its method", p, name, name)
			}
		}
	}
}

// The lead's final block and the PO's event loop agree: every Next: value
// the lead may end with has a PO action, and every agent type the lead puts
// in Spawn: for tasks and reviews is one the PO spawns.
func TestLeadFinalBlockHasPOActions(t *testing.T) {
	texts := pluginTexts(t)
	lead, po := texts["plugin/skills/lead/SKILL.md"], texts["plugin/skills/work-ticket/SKILL.md"]
	m := regexp.MustCompile(`(?m)^Next: (.+)$`).FindStringSubmatch(lead)
	if m == nil {
		t.Fatal("lead: no Next: line in the final block")
	}
	for _, v := range strings.Split(m[1], " | ") {
		v = strings.TrimSpace(v)
		if !strings.Contains(po, "`"+v+"` →") {
			t.Errorf("PO event loop has no action for Next: %s", v)
		}
	}
	for _, a := range []string{"tickets:task", "tickets:mechanical", "tickets:reviewer", "tickets:reviewer-deep"} {
		if !strings.Contains(lead, "`"+a+"`") {
			t.Errorf("lead: doesn't name %s", a)
		}
	}
	if !strings.Contains(po, "`tickets:task`, `tickets:mechanical`, `tickets:reviewer(-deep)`") {
		t.Error("PO: the agents it spawns for the lead aren't listed")
	}
}

// Fixes from the PO/lead QA round (MAN-16, D1–D5): no read-only design
// agent whose reply can't reach the lead, liveness by session id with
// orphaned rows, a relay trim rule that can meet Open ≤30, relay.md and
// board.md created at kickoff, and pr-feedback merging as the lead does.
// The relay and board states agree wherever they're listed.
func TestPOLeadQAFixes(t *testing.T) {
	texts := pluginTexts(t)
	po, kickoff := texts["plugin/skills/work-ticket/SKILL.md"], texts["plugin/skills/work-ticket/kickoff.md"]
	lead, handover := texts["plugin/skills/lead/SKILL.md"], texts["plugin/skills/lead/handover.md"]
	refresh := texts["plugin/skills/po-refresh/SKILL.md"]
	tpl := func(name string) string {
		data, err := fs.ReadFile(Scaffold, "vault-scaffold/templates/"+name+".md")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	data, err := fs.ReadFile(Scaffold, "vault-scaffold/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	agents := string(data)
	has := func(name, text string, want ...string) {
		t.Helper()
		for _, s := range want {
			if !strings.Contains(text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}

	// D1: the built-in Plan agent is read-only and its reply reaches the PO only.
	planSpawn := regexp.MustCompile("built-in `Plan`|`Plan` agent|agent: Plan\\b")
	for p, s := range texts {
		if planSpawn.MatchString(s) {
			t.Errorf("%s asks for a built-in Plan agent", p)
		}
	}
	has("lead", lead, "You draft it yourself", "`tickets:reviewer-deep` pass on the contract file")

	// D2: liveness by session id, orphaned rows, no lead at review or done.
	has("PO", po, "`$CLAUDE_CODE_SESSION_ID`", "po: session:", "never by a ping timeout", "`orphaned`",
		"set `agents: []` and `lead: none`", "orphaned tasks NN", "replace it only after its completion notice arrived",
		"`lead: none` with `status: review` or `done`: spawn no lead unless", "  session: <$CLAUDE_CODE_SESSION_ID")
	for _, s := range []string{"no answer means stale", "doesn't answer is\nstale"} {
		if strings.Contains(po, s) || strings.Contains(refresh, s) {
			t.Errorf("PO/po-refresh still treat silence as a stale lead: %q", s)
		}
	}
	has("po-refresh", refresh, "`po: session:` still matches", "completion notice arrived")
	has("lead", lead, "`orphaned` lines", "re-plan it", "`partial` or\n`orphaned`")
	has("lead hand-over", handover, "`partial` or `orphaned` line")
	has("board template", tpl("board"), "\n  session: \"\"\n", "running, orphaned,")
	has("AGENTS.md", agents, "`po: session:`", "`orphaned`", "`status`\n   (it runs the gate")

	// D3: decisions and answers close once copied; the trim can reach ≤30.
	has("PO", po, "`copied`, `answered` and `planned` entries", "or that have no line",
		"oldest first, until `## Open` is ≤30", "one entry per\ntheme")
	has("lead", lead, "set it `copied`", "set the answer entry `copied`", "change the state (last field) of any line")
	states := regexp.MustCompile(`<open\|[a-z|]+>`)
	want := "<open|planned|answered|copied>"
	for name, text := range map[string]string{"PO": po, "lead": lead, "relay template": tpl("relay"), "AGENTS.md": agents} {
		got := states.FindAllString(text, -1)
		if len(got) == 0 {
			t.Errorf("%s: no relay state list", name)
		}
		for _, g := range got {
			if g != want {
				t.Errorf("%s: relay states %s, want %s", name, g, want)
			}
		}
	}

	// D4: the PO creates relay.md and board.md from their templates at kickoff.
	has("kickoff", kickoff, "`<vault>/templates/relay.md`, `board.md` and", "whether or not a grill follows")
	has("PO", po, "it creates `relay.md` and `board.md`", "At once record it")
	has("lead", lead, "the PO did the kickoff and created `board.md`")

	// D5: pr-feedback merges fix tasks as the lead does.
	pr := texts["plugin/skills/pr-feedback/SKILL.md"]
	if regexp.MustCompile("as in `work-ticket`( §|\\))").MatchString(pr) {
		t.Error("pr-feedback: still points to work-ticket for task worktrees or merges")
	}
	has("pr-feedback", pr, "`lead/SKILL.md` \"5. Merge\"")

	// Board row states: every state the lead's hand-over checks is listed in the template.
	listed := map[string]bool{}
	for _, s := range []string{"planned", "running", "orphaned", "merged", "partial", "blocked", "dropped", "pending"} {
		listed[s] = strings.Contains(tpl("board"), s)
		if !listed[s] {
			t.Errorf("board template: state %q not listed", s)
		}
	}
}

// The disposition gate (contract vault-checklists-v1 v1.1) is the same in the
// lead skill and the PO's hand-over, and flags a missing section, a closed
// state without its word, and a leftover open line.
const checklistGate = `for f in tickets/<ID>/reviews/*.md; do
  [ -e "$f" ] || continue
  rg -q '^## Dispositions' "$f" || echo "$f: no Dispositions"
  for id in $(rg -o '^- (W[0-9]+-[a-z0-9-]+-[MSC][0-9]+) · ' -r '$1' "$f" | sort -u); do
    rg -q "^- \[x\] $id · (resolved · |deferred → \[\[.*#deferred)|^- \[-\] $id · kept: " "$f" || echo "$f: $id"
    rg -q "^- \[.\] $id · (open|fixing)" "$f" && echo "$f: $id open line"
  done
done`

func TestDispositionGateLoopMatches(t *testing.T) {
	texts := pluginTexts(t)
	for _, p := range []string{"plugin/skills/lead/SKILL.md", "plugin/skills/work-ticket/handover.md"} {
		if !strings.Contains(texts[p], checklistGate) {
			t.Errorf("%s: gate loop differs from the contract", p)
		}
	}
	// The hand-over step, the review template and recall follow the contract too.
	if !strings.Contains(texts["plugin/skills/lead/handover.md"], "no Dispositions") ||
		!strings.Contains(texts["plugin/skills/lead/handover.md"], "#deferred") {
		t.Error("handover.md: the gate must include the no Dispositions, open-line and #deferred checks")
	}
	tpl, err := fs.ReadFile(Scaffold, "vault-scaffold/templates/review.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Dispositions", "`- [ ] <ID> · open`", "`- [/] <ID> · fixing · task NN`",
		"`- [x] <ID> · resolved · task NN (<sha>) ✅ <date>`", "`- [-] <ID> · kept: <reason ≤150 chars> ❌ <date>`",
		"`- [x] <ID> · deferred → [[<ticket>]] · <why> #deferred ✅ <date>`"} {
		if !strings.Contains(string(tpl), s) {
			t.Errorf("templates/review.md: missing %s", s)
		}
	}
}

// The contract's line forms appear in the lead, hand-over, wave-review and
// recall skills, the reviewers and the templates (vault-checklists-v1).
func TestChecklistLineForms(t *testing.T) {
	texts := pluginTexts(t)
	want := map[string][]string{
		"plugin/skills/lead/SKILL.md": {
			"`vault-checklists-v1`", "rg -n '^- \\[.\\] [0-9]+ · |^(status|wave|last-reviewed):'",
			"`rg -n '^- \\[x\\] [0-9]+ · (planned|running|orphaned|partial|blocked|pending)|^- \\[[ /]\\] [0-9]+ · merged '",
			"rg -q \"^- \\[<c>\\] NN · <status> · \" board.md || echo \"NN not updated\"",
			"· (open|fixing)' tickets/<ID>/reviews/`", "- [ ] B-nn · w<N> · <repo> · <item> · <finding id> ➕ <date>",
			"rg -o 'B-[0-9]+' backlog.md | sort -V | tail -1", "`### w<N>`",
			"`- w<N> fix · <repo> · <from>..<to> · - · <what you checked> · -`",
			"- [x] <ID> · deferred → [[<ticket>]] · <why> #deferred ✅ <date>",
		},
		"plugin/skills/lead/handover.md":       {"#deferred", "- [ ] B-nn · w<N>"},
		"plugin/skills/wave-review/SKILL.md":   {"`- [ ] <ID> · open`", "(open|fixing)"},
		"plugin/skills/recall/SKILL.md":        {"(open|fixing)", "<ID>-status.md"},
		"plugin/skills/work-ticket/SKILL.md":   {"<ID>-status.md", "- \\[\\/\\] [0-9]+ · )running · "},
		"plugin/skills/work-ticket/kickoff.md": {"status.md", "<ID>-status.md", `s/TICKET-ID/<ID>/g`},
		"plugin/agents/reviewer.md":            {"`- [ ] <ID> · open`"},
		"plugin/agents/reviewer-deep.md":       {"`- [ ] <ID> · open`"},
	}
	for p, ss := range want {
		for _, s := range ss {
			if !strings.Contains(texts[p], s) {
				t.Errorf("%s: missing %q", p, s)
			}
		}
	}
	// No file names the removed views (the vault AGENTS.md is task 10's).
	for p, s := range texts {
		for _, old := range []string{"pending.md", "done.md", "follow-ups.md"} {
			if strings.Contains(s, old) {
				t.Errorf("%s still mentions %s", p, old)
			}
		}
	}
	err := fs.WalkDir(Scaffold, "vault-scaffold", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "vault-scaffold/AGENTS.md" {
			return err
		}
		data, err := fs.ReadFile(Scaffold, p)
		if err != nil {
			return err
		}
		for _, old := range []string{"pending.md", "done.md", "follow-ups.md"} {
			if strings.Contains(string(data), old) {
				t.Errorf("%s still mentions %s", p, old)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// shBlock returns the text of a fenced block from start up to its closing fence.
func shBlock(t *testing.T, text, start string) string {
	t.Helper()
	i := strings.Index(text, start)
	if i < 0 {
		t.Fatalf("no sh block starting %q", start)
	}
	j := strings.Index(text[i:], "\n```")
	if j < 0 {
		t.Fatal("unterminated sh block")
	}
	return text[i : i+j]
}

func requireTool(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Skipf("%s not available: %v", name, err)
	}
	if name == "sed" && !strings.Contains(string(out), "GNU") {
		t.Skip("needs GNU sed")
	}
}

// The lead's one sed recipe, run on fixture board lines: running to merged
// twice (the date is replaced, not stacked), merged to partial (the date goes),
// dropped, and a plain-text task line.
func TestBoardSedRecipe(t *testing.T) {
	requireTool(t, "sed", "--version")
	requireTool(t, "rg", "--version")
	lead := pluginTexts(t)["plugin/skills/lead/SKILL.md"]
	recipe := shBlock(t, lead, `sed -i -E "/^- \[.\] NN · `)
	run := func(board, nn, c, status, date string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "board.md")
		if err := os.WriteFile(path, []byte(board), 0o644); err != nil {
			t.Fatal(err)
		}
		script := strings.NewReplacer("NN", nn, "<c>", strings.ReplaceAll(c, "/", `\/`), "<status>", status, "<date>", date).Replace(recipe)
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("recipe: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "not updated") {
			t.Fatalf("recipe reported no change: %s", out)
		}
		data, _ := os.ReadFile(path)
		return string(data)
	}
	const plan = "- [ ] 01 · planned · [[T-1-01-api]] · acme-api · w1 · deps - · R-03 ➕ 2026-10-25\n" +
		"- [ ] 02 · planned · [[T-1-02-ui]] · acme-ui · w1 · deps 01 · plain text task ➕ 2026-10-25\n"
	b := run(plan, "01", "/", "running", "")
	if !strings.Contains(b, "- [/] 01 · running · [[T-1-01-api]] · acme-api · w1 · deps - · R-03 ➕ 2026-10-25\n") {
		t.Errorf("running: %q", b)
	}
	b = run(b, "01", "x", "merged fcf8d61", " ✅ 2026-10-26")
	b = run(b, "01", "x", "merged abc1234", " ✅ 2026-10-27")
	if !strings.Contains(b, "- [x] 01 · merged abc1234 · [[T-1-01-api]] · acme-api · w1 · deps - · R-03 ➕ 2026-10-25 ✅ 2026-10-27\n") ||
		strings.Count(b, "✅") != 1 {
		t.Errorf("merged twice: %q", b)
	}
	b = run(b, "01", "/", "partial", "")
	if !strings.Contains(b, "- [/] 01 · partial · [[T-1-01-api]] · acme-api · w1 · deps - · R-03 ➕ 2026-10-25\n") {
		t.Errorf("merged to partial: %q", b)
	}
	b = run(b, "02", "-", "dropped", " ❌ 2026-10-28")
	if !strings.Contains(b, "- [-] 02 · dropped · [[T-1-02-ui]] · acme-ui · w1 · deps 01 · plain text task ➕ 2026-10-25 ❌ 2026-10-28\n") {
		t.Errorf("dropped: %q", b)
	}
	// The recipe fails loudly for a line that doesn't exist.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "board.md"), []byte(plan), 0o644)
	cmd := exec.Command("sh", "-c", strings.NewReplacer("NN", "09", "<c>", "x", "<status>", "merged -", "<date>", "").Replace(recipe))
	cmd.Dir = dir
	if out, _ := cmd.CombinedOutput(); !strings.Contains(string(out), "not updated") {
		t.Errorf("missing line not reported: %q", out)
	}
}

// The gate run on fixture reviews: closed lines pass; an open line, a box-only
// change, a deferral without #deferred and a missing section are printed; no
// reviews print nothing.
func TestDispositionGateRuns(t *testing.T) {
	requireTool(t, "rg", "--version")
	lead := pluginTexts(t)["plugin/skills/lead/SKILL.md"]
	gate := strings.ReplaceAll(shBlock(t, lead, "for f in tickets/<ID>/reviews/*.md; do"), "<ID>", "T-1")
	run := func(files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		rev := filepath.Join(dir, "tickets", "T-1", "reviews")
		if err := os.MkdirAll(rev, 0o755); err != nil {
			t.Fatal(err)
		}
		for n, c := range files {
			if err := os.WriteFile(filepath.Join(rev, n), []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("sh", "-c", gate)
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	const findings = "## Must-fix\n- W1-acme-api-M01 · a.go:1 · p · f · t\n## Should-fix\n- W1-acme-api-S01 · a.go:2 · p · f · t\n## Consider\n- W1-acme-api-C01 · a.go:3 · p · f · t\n\n## Dispositions\n"
	const closed = "- [x] W1-acme-api-M01 · resolved · task 02 (1cf4eb1) ✅ 2026-10-26\n" +
		"- [-] W1-acme-api-S01 · kept: intended ❌ 2026-10-26\n" +
		"- [x] W1-acme-api-C01 · deferred → [[PROJ-40]] · later #deferred ✅ 2026-10-26\n"
	if out := run(nil); out != "" {
		t.Errorf("no reviews: %q", out)
	}
	if out := run(map[string]string{"w1.md": findings + closed}); out != "" {
		t.Errorf("all closed: %q", out)
	}
	open := findings + strings.Replace(closed, "- [x] W1-acme-api-M01 · resolved · task 02 (1cf4eb1) ✅ 2026-10-26", "- [ ] W1-acme-api-M01 · open", 1)
	if out := run(map[string]string{"w1.md": open}); !strings.Contains(out, "w1.md: W1-acme-api-M01") {
		t.Errorf("open line: %q", out)
	}
	// A click that checks only the box: the word still says open.
	box := findings + strings.Replace(closed, "- [x] W1-acme-api-M01 · resolved · task 02 (1cf4eb1) ✅ 2026-10-26", "- [x] W1-acme-api-M01 · open", 1)
	if out := run(map[string]string{"w1.md": box}); !strings.Contains(out, "w1.md: W1-acme-api-M01") {
		t.Errorf("box-only change: %q", out)
	}
	// A resolved word on an unchecked box is still an open line.
	unticked := findings + strings.Replace(closed, "- [x] W1-acme-api-M01 · resolved", "- [ ] W1-acme-api-M01 · resolved", 1)
	if out := run(map[string]string{"w1.md": unticked}); !strings.Contains(out, "w1.md: W1-acme-api-M01") {
		t.Errorf("unchecked resolved: %q", out)
	}
	nodef := findings + strings.Replace(closed, " #deferred", "", 1)
	if out := run(map[string]string{"w1.md": nodef}); !strings.Contains(out, "w1.md: W1-acme-api-C01") {
		t.Errorf("deferred without #deferred: %q", out)
	}
	if out := run(map[string]string{"w1.md": "## Must-fix\n- W1-acme-api-M01 · a · p · f · t\n"}); !strings.Contains(out, "no Dispositions") {
		t.Errorf("missing section: %q", out)
	}
	if out := run(map[string]string{"w1.md": "## Summary\nclean\n\n## Dispositions\n"}); out != "" {
		t.Errorf("review without findings: %q", out)
	}
}

// MAN-2: the PO wakes the lead once per wave, not per finished agent. Task
// and reviewer notices are buffered by wave (po: agents carry it, po:
// finished keeps the unsent ones across /compact); blocked or failed tasks
// and task→lead questions still go at once; the lead merges the whole wave
// in one run and merges the rest when one branch fails.
func TestWaveBatchedWakeups(t *testing.T) {
	texts := pluginTexts(t)
	po, lead := texts["plugin/skills/work-ticket/SKILL.md"], texts["plugin/skills/lead/SKILL.md"]
	refresh := texts["plugin/skills/po-refresh/SKILL.md"]
	// Whitespace-insensitive, so a reflow of the skill text does not break it.
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	has := func(name, text string, want ...string) {
		t.Helper()
		for _, s := range want {
			if !strings.Contains(norm(text), norm(s)) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
	}
	tplData, err := fs.ReadFile(Scaffold, "vault-scaffold/templates/board.md")
	if err != nil {
		t.Fatal(err)
	}
	agentsData, err := fs.ReadFile(Scaffold, "vault-scaffold/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}

	// The PO batches per wave and kind, and sends blocked/failed at once.
	has("PO", po, "batch it per wave", "wake it **once per wave**", "**no\n  entry of the same wave and kind**",
		"\"wave 2 finished: tasks 04 done, 05 partial,", "\"wave 2 reviews\n  written: <slug>, <slug>\"",
		"A task reporting `blocked` or failed (or dying) goes to the lead **at once**", "no early merge, no timeout",
		"at once,\n  never batched", "with its `wave:`", "  finished:\n", "`finished: []`",
		// Review W1 S01/S02: a blocked notice names the unsent finished tasks and can close the wave.
		"finished, unsent: 05", "also right after such a notice")
	for _, s := range []string{"SendMessage the lead one line: \"task 03 finished", "forward it like any finished task"} {
		if strings.Contains(po, s) {
			t.Errorf("PO still wakes the lead per task: %q", s)
		}
	}

	// The lead tags spawns with their wave and merges the whole wave in one run.
	has("lead", lead, "carries `wave: <N>`", "  wave: 3\n  prompt: Task 03:", "The PO wakes you **once per wave**",
		"Merge **all** of them in this run", "never\nexpect one message per task", "go on merging the rest of the wave",
		"all the wave's reviews are written",
		// Review W1 S02/S03: merge only from the wave line; fix tasks get a defined wave.
		"merge the finished ones only from the wave line", "A fix or follow-up task takes the wave of the work it fixes")
	if strings.Contains(lead, "When the PO tells you a task finished") {
		t.Error("lead still expects one message per finished task")
	}

	// po-refresh checkpoints both lists; the board template and AGENTS.md carry them.
	has("po-refresh", refresh, "w<N>|- · task NN|review <repo>|analyst", "`finished:`", "the buffered notices")
	has("board template", string(tplData), "\n  agents: []\n  finished: []\n")
	has("AGENTS.md", string(agentsData), "wakes the lead once per wave")
}

// R-08: no agent files a follow-up ticket without a per-item answer from the user.
func TestFollowUpsAskedPerItem(t *testing.T) {
	texts := pluginTexts(t)
	for p, want := range map[string]string{
		"plugin/skills/lead/SKILL.md":        "Never file a ticket on your own",
		"plugin/skills/lead/handover.md":     "Never run `ct new` unasked",
		"plugin/skills/kb/SKILL.md":          "Never run `ct new` without that answer",
		"plugin/skills/retro/SKILL.md":       "never file unasked",
		"plugin/skills/work-ticket/SKILL.md": "nothing is filed with `ct new` before the answer",
	} {
		if !strings.Contains(texts[p], want) {
			t.Errorf("%s: missing %q", p, want)
		}
	}
	if strings.Contains(texts["plugin/skills/lead/handover.md"], "at most 5 candidate") {
		t.Error("handover.md: still groups into candidates")
	}
}

// The A-D follow-up options, the batching rule and the scope read the same in
// every file that states them.
func TestFollowUpAskWording(t *testing.T) {
	texts := pluginTexts(t)
	for _, p := range []string{"vault-scaffold/AGENTS.md", "vault-scaffold/templates/backlog.md"} {
		data, err := fs.ReadFile(Scaffold, p)
		if err != nil {
			t.Fatal(err)
		}
		texts[p] = string(data)
	}
	const opts = "A, this ticket; B, a new ticket; C, an existing ticket (name it); D, leave it in the backlog (backlog items only)"
	for _, p := range []string{
		"plugin/skills/lead/SKILL.md", "plugin/skills/lead/handover.md", "plugin/skills/work-ticket/SKILL.md",
		"vault-scaffold/AGENTS.md", "vault-scaffold/templates/backlog.md",
	} {
		if !strings.Contains(texts[p], opts) {
			t.Errorf("%s: missing the A-D options wording", p)
		}
	}
	// Batching (S02): the same rule in the lead and the PO texts.
	for _, p := range []string{"plugin/skills/lead/SKILL.md", "plugin/skills/lead/handover.md", "plugin/skills/work-ticket/SKILL.md"} {
		for _, want := range []string{
			"Up to 4 items: one",
			"More than 4: one question per repo or theme, listing each item with ",
			"proposed option (A-D); the user accepts it or overrides it per item",
			"at most 4 questions per",
		} {
			if !strings.Contains(texts[p], want) {
				t.Errorf("%s: missing batching rule %q", p, want)
			}
		}
	}
	// Scope (S01).
	if !strings.Contains(texts["plugin/skills/lead/SKILL.md"], "items you would not fix in this") ||
		!strings.Contains(texts["plugin/skills/work-ticket/SKILL.md"], "never fix tasks or `kept:`") {
		t.Error("lead and PO texts must scope the ask to deferral candidates and backlog items")
	}
	// Other places that mention the ask, and the wave review that never files.
	if !strings.Contains(texts["plugin/skills/wave-review/SKILL.md"], "you never file tickets") {
		t.Error("wave-review: reviewer must never file tickets")
	}
	if !strings.Contains(texts["plugin/skills/retro/SKILL.md"], "a retro item only becomes\na backlog item") {
		t.Error("retro: in a ticket a retro item only becomes a backlog item")
	}
	if !strings.Contains(texts["plugin/skills/lead/SKILL.md"], "Never file a ticket on your own") ||
		!strings.Contains(texts["vault-scaffold/AGENTS.md"], "Agents never file a follow-up ticket on their own") {
		t.Error("lead skill and scaffold AGENTS.md must forbid unasked filing")
	}
	for _, bad := range []string{"you pick become follow-up", "Writing a `deferred` line means filing"} {
		for p, s := range texts {
			if strings.Contains(s, bad) {
				t.Errorf("%s: contains %q", p, bad)
			}
		}
	}
}
