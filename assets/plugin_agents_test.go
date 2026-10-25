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

// The wave workflow's agents (MAN-16 AC1): the role agents are gone, and
// each agent pins its model, lists its tools explicitly without Agent, and
// preloads a brief skill the plugin ships.
func TestPluginAgents(t *testing.T) {
	for _, gone := range []string{"product-owner", "architect", "developer", "qa"} {
		if _, err := fs.Stat(Files, "plugin/agents/"+gone+".md"); err == nil {
			t.Errorf("role agent %s.md still ships", gone)
		}
	}
	want := map[string]bool{"task": true, "mechanical": true, "reviewer": true, "reviewer-deep": true}
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
		if v, _, ok := field(fm, "model"); !ok || v == "" || v == "inherit" {
			t.Errorf("%s: no fixed model", e.Name())
		}
		tools, _, ok := field(fm, "tools")
		if !ok || tools == "" {
			t.Errorf("%s: no explicit tools list", e.Name())
		}
		for _, tool := range strings.Split(tools, ",") {
			tool = strings.TrimSpace(tool)
			if tool == "Agent" || tool == "Task" || strings.HasPrefix(tool, "mcp__graphify__") || strings.HasPrefix(tool, "mcp__atlassian__") {
				t.Errorf("%s: tool %s isn't allowed", e.Name(), tool)
			}
		}
		_, skills, _ := field(fm, "skills")
		if len(skills) == 0 {
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

// The briefs and the lead's playbook agree with the ticket-folder contract:
// review caps and path (AC6), the files work-ticket reads on demand, and
// the templates it names.
func TestWaveSkillsReferences(t *testing.T) {
	read := func(p string) string {
		data, err := fs.ReadFile(Files, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	review := read("plugin/skills/wave-review/SKILL.md")
	for _, s := range []string{"3 must-fix, 6 should-fix, 5 consider", "tickets/<ID>/reviews/wave-<N>-<slug>.md"} {
		if !strings.Contains(review, s) {
			t.Errorf("wave-review: missing %q", s)
		}
	}
	lead := read("plugin/skills/work-ticket/SKILL.md")
	for _, f := range []string{"kickoff.md", "handover.md"} {
		if !strings.Contains(lead, "`"+f+"`") {
			t.Errorf("work-ticket doesn't point at %s", f)
		}
		read("plugin/skills/work-ticket/" + f)
	}
	for _, s := range []string{"4 agents in flight", "`owns` overlap never share a wave", "--no-ff", "≤3 calls"} {
		if !strings.Contains(lead, s) {
			t.Errorf("work-ticket: missing %q", s)
		}
	}
	for _, tpl := range []string{"plan", "board", "task", "review", "backlog"} {
		if !strings.Contains(lead, "`"+tpl+"`") {
			t.Errorf("work-ticket doesn't name template %s", tpl)
		}
		if _, err := fs.Stat(Scaffold, "vault-scaffold/templates/"+tpl+".md"); err != nil {
			t.Errorf("template %s.md isn't shipped", tpl)
		}
	}
	for _, tpl := range []string{"plan", "board", "task", "review", "backlog"} {
		data, _ := fs.ReadFile(Scaffold, "vault-scaffold/templates/"+tpl+".md")
		if strings.Contains(string(data), "- [ ]") {
			t.Errorf("template %s.md has a checkbox (pending.md would list it)", tpl)
		}
		if !strings.Contains(string(data), "type: "+tpl+"\n") {
			t.Errorf("template %s.md: no type: %s", tpl, tpl)
		}
	}
}
