package assets

import (
	"io/fs"
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
	has("lead", lead, "`orphaned` rows", "re-plan it", "`partial` or\n`orphaned`")
	has("lead hand-over", handover, "`partial` or `orphaned` row")
	has("board template", tpl("board"), "\n  session: \"\"\n", "· orphaned ·")
	has("AGENTS.md", agents, "`po: session:`", "`orphaned`", "`status`\n   (it runs the gate")

	// D3: decisions and answers close once copied; the trim can reach ≤30.
	has("PO", po, "`copied`, `answered` and `planned` entries", "or that have no row",
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
	has("kickoff", kickoff, "`<vault>/templates/relay.md` and `board.md`", "whether or not a grill follows")
	has("PO", po, "it creates `relay.md` and `board.md`", "At once record it")
	has("lead", lead, "the PO did the kickoff and created `board.md`")

	// D5: pr-feedback merges fix tasks as the lead does.
	pr := texts["plugin/skills/pr-feedback/SKILL.md"]
	if regexp.MustCompile("as in `work-ticket`( §|\\))").MatchString(pr) {
		t.Error("pr-feedback: still points to work-ticket for task worktrees or merges")
	}
	has("pr-feedback", pr, "`lead/SKILL.md` \"5. Merge\"")

	// Board row states: every state the lead's hand-over checks is listed in the template.
	m := regexp.MustCompile(`Status: ([^.]+)\.`).FindStringSubmatch(tpl("board"))
	if m == nil {
		t.Fatal("board template: no Status list")
	}
	listed := map[string]bool{}
	for _, s := range strings.Split(m[1], " · ") {
		listed[strings.Fields(s)[0]] = true
	}
	for _, s := range []string{"planned", "running", "orphaned", "merged", "partial", "blocked", "dropped", "pending"} {
		if !listed[s] {
			t.Errorf("board template: state %q not listed", s)
		}
	}
}

// The review-disposition gate loop (contract review-disposition-v1 v1.1) is
// the same in the lead skill and the PO's hand-over, and flags a missing section.
func TestDispositionGateLoopMatches(t *testing.T) {
	const gate = `for f in tickets/<ID>/reviews/*.md; do
  rg -q '^## Dispositions' "$f" || echo "$f: no Dispositions"
  for id in $(rg -o '^- (W[0-9]+-[a-z0-9-]+-[MSC][0-9]+) · ' -r '$1' "$f" | sort -u); do
    rg -q "^- $id · (resolved|deferred) · " "$f" || echo "$f: $id"
  done
done`
	texts := pluginTexts(t)
	for _, p := range []string{"plugin/skills/lead/SKILL.md", "plugin/skills/work-ticket/handover.md"} {
		if !strings.Contains(texts[p], gate) {
			t.Errorf("%s: gate loop differs from the contract", p)
		}
	}
	// W10 C01, S01: the hand-over step, the review template and recall follow the contract too.
	if !strings.Contains(texts["plugin/skills/lead/handover.md"], "no Dispositions") {
		t.Error("handover.md: the gate must include the no Dispositions check")
	}
	if !strings.Contains(texts["plugin/skills/recall/SKILL.md"], "-M[0-9]+ · open$") {
		t.Error("recall: open must-fixes come from the Dispositions `· open` lines")
	}
	tpl, err := fs.ReadFile(Scaffold, "vault-scaffold/templates/review.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Dispositions", "`<ID> · resolved · kept: <reason>`", "`<ID> · deferred · [[<ticket>]] · <why>`"} {
		if !strings.Contains(string(tpl), s) {
			t.Errorf("templates/review.md: missing %s", s)
		}
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
