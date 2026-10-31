package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zero4573/tix/assets"
)

func TestKiroText(t *testing.T) {
	in := "Run /tix:work-ticket MAN-1, then the `tix:lead` agent (set in `.claude/settings.json`); tickets: stay."
	want := "Run /tix-work-ticket MAN-1, then the `tix-lead` agent (set in the session's environment); tickets: stay."
	if got := KiroText(in); got != want {
		t.Errorf("KiroText:\n got %q\nwant %q", got, want)
	}
}

func TestSelect(t *testing.T) {
	t.Setenv("TIX_AGENT", "")
	for _, c := range []struct{ flag, env, vault, want string }{
		{"", "", "", Claude},
		{"", "", "kiro", Kiro},
		{"", "kiro", "claude", Kiro},
		{"claude", "kiro", "kiro", Claude},
	} {
		t.Setenv("TIX_AGENT", c.env)
		h, err := Select(c.flag, c.vault)
		if err != nil || h.Name() != c.want {
			t.Errorf("Select(%q, env %q, vault %q) = %v, %v; want %s", c.flag, c.env, c.vault, h, err, c.want)
		}
	}
	if _, err := Select("vim", ""); err == nil {
		t.Error("an unknown agent was accepted")
	}
}

// The folders go after what the environment has, once each
func TestSandboxEnv(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv("SANDBOX_ADD_DIRS", "/mine"+sep+"/v")
	t.Setenv("SANDBOX_RO_PATHS", "")
	env := sandboxEnv(Session{Dirs: []string{"/v", "/r/"}, ReadOnly: []string{"/m"}, ReposRoot: "/p"})
	if got, want := env["SANDBOX_ADD_DIRS"], "/mine"+sep+"/v"+sep+"/r/"; got != want {
		t.Errorf("SANDBOX_ADD_DIRS = %q, want %q", got, want)
	}
	if got, want := env["SANDBOX_RO_PATHS"], "/m"+sep+"/p"; got != want {
		t.Errorf("SANDBOX_RO_PATHS = %q, want %q", got, want)
	}
}

// The shipped plugin, rendered for kiro: every skill and agent, no
// tix:<name> left, and every tix-<name> they name is a rendered skill or
// agent (or the session's own agent, tix)
func TestRenderKiroPlugin(t *testing.T) {
	t.Setenv("TIX_CACHE", t.TempDir())
	plugin, err := assets.Materialize("plugin")
	if err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(t.TempDir(), ".kiro")
	agents, err := RenderKiro(plugin, dot)
	if err != nil {
		t.Fatal(err)
	}
	srcAgents, _ := filepath.Glob(filepath.Join(plugin, "agents", "*.md"))
	if len(agents) == 0 || len(agents) != len(srcAgents) {
		t.Fatalf("agents %v, from %d files", agents, len(srcAgents))
	}
	known := map[string]bool{"tix": true}
	for _, a := range agents {
		known[a] = true
		// every agent gets the MCP servers of kiro's mcp.json, as the
		// session's own agent does
		data, err := os.ReadFile(filepath.Join(dot, "agents", a+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("%s.json: %v", a, err)
		}
		if cfg["includeMcpJson"] != true {
			t.Errorf("%s.json: includeMcpJson = %v, want true", a, cfg["includeMcpJson"])
		}
		// the lead and the deep reviewer on opus, the rest on kiro's router
		want := "auto"
		if a == "tix-lead" || a == "tix-reviewer-deep" {
			want = "claude-opus-5.5"
		}
		if cfg["model"] != want {
			t.Errorf("%s.json: model = %v, want %s", a, cfg["model"], want)
		}
	}
	skills, _ := os.ReadDir(filepath.Join(dot, "skills"))
	srcSkills, _ := os.ReadDir(filepath.Join(plugin, "skills"))
	if len(skills) != len(srcSkills) {
		t.Fatalf("%d skills rendered, %d in the plugin", len(skills), len(srcSkills))
	}
	for _, s := range skills {
		known[s.Name()] = true
		data, err := os.ReadFile(filepath.Join(dot, "skills", s.Name(), "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "\nname: "+s.Name()+"\n") || !strings.Contains(string(data), "In kiro:") {
			t.Errorf("%s/SKILL.md: name or notes missing", s.Name())
		}
	}
	ref := regexp.MustCompile(`tix-([a-z][a-z0-9-]*[a-z0-9])`)
	err = filepath.WalkDir(dot, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if regexp.MustCompile(`(^|[^a-z])tix:[a-z]`).Match(data) {
			t.Errorf("%s still names tix:<name>", p)
		}
		for _, m := range ref.FindAllStringSubmatch(string(data), -1) {
			name := "tix-" + m[1]
			// tix-improvements is the vault folder, not a skill or agent
			if !known[name] && !strings.HasPrefix(name, "tix-tasks") && name != "tix-improvements" {
				t.Errorf("%s names %s, which isn't a rendered skill or agent", p, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
