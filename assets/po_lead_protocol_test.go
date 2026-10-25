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
