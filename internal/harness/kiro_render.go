package harness

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/tix/internal/fsx"
)

// The plugin is written once, for Claude Code (assets/plugin: skills/,
// agents/*.md, hooks/hooks.json). kiro gets the same skills and agents,
// rendered into a workspace's .kiro/ at every start (RenderKiro):
//
//   - skills/<name>/ becomes .kiro/skills/tix-<name>/ (kiro skill names
//     can't hold a ':'), its SKILL.md headed by kiroToolNotes, which maps
//     the Claude tool names the skills use onto kiro's
//   - agents/<name>.md becomes .kiro/agents/tix-<name>.json, its body the
//     agent's prompt; kiro offers it to the session as the
//     subagent_tix-<name> tool
//   - every /tix:<name> and tix:<name> in them becomes /tix-<name>, tix-<name>
//
// So there is one source to maintain; what differs between the CLIs is the
// small table below and this renderer.

// kiroToolNotes heads every skill and agent rendered for kiro.
const kiroToolNotes = "> **In kiro:** this playbook names Claude Code's tools. Use kiro's instead:\n" +
	"> Read/Glob/Grep → `fs_read`, `glob`, `grep`; Edit/Write → `fs_write`; Bash → `execute_bash`;\n" +
	"> the Agent/Task tool and `tix-<agent>` agents → the `subagent_tix-<agent>` tool;\n" +
	"> the Skill tool → the `/tix-<skill>` command or the skill's file in `.kiro/skills/`;\n" +
	"> AskUserQuestion → ask in plain text and wait for the answer;\n" +
	"> SendMessage (to a running agent) isn't available: start the agent again with the\n" +
	"> new instructions, or hand them over through the files the workflow already uses\n" +
	"> (`relay.md`, `board.md`).\n\n"

var kiroRefs = regexp.MustCompile(`(/?)tix:([a-z][a-z0-9-]*)`)

// KiroText is text of the plugin (or of a tix-written instructions file)
// with its tix:<name> references in kiro's form.
func KiroText(s string) string {
	s = kiroRefs.ReplaceAllString(s, "${1}tix-${2}")
	return strings.ReplaceAll(s, "`.claude/settings.json`", "the session's environment")
}

// KiroSkill is the kiro form of a skill command: /tix-<skill>.
func KiroSkill(skill string) string { return "/tix-" + skill }

// RenderKiro writes plugin's skills and agents for kiro into dotKiro (a
// workspace's .kiro): skills/tix-*/ and agents/tix-*.json, each replaced;
// the rest of dotKiro (tix.json, the user's own) is left alone. It returns
// the agents' names (tix-<name>).
func RenderKiro(plugin, dotKiro string) ([]string, error) {
	skillsOut := filepath.Join(dotKiro, "skills")
	agentsOut := filepath.Join(dotKiro, "agents")
	for _, d := range []string{skillsOut, agentsOut} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	old, _ := filepath.Glob(filepath.Join(skillsOut, "tix-*"))
	for _, d := range old {
		if err := fsx.RemoveAll(d); err != nil {
			return nil, err
		}
	}
	oldAgents, _ := filepath.Glob(filepath.Join(agentsOut, "tix-*.json"))
	for _, f := range oldAgents {
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}

	src := filepath.Join(plugin, "skills")
	if _, err := os.Stat(src); err != nil {
		src = ""
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if src == "" {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		parts[0] = "tix-" + parts[0]
		target := filepath.Join(append([]string{skillsOut}, parts...)...)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s isn't a regular file, not rendering it", p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.HasSuffix(p, ".md") {
			text = KiroText(text)
			if filepath.Base(p) == "SKILL.md" {
				text = kiroSkillFile(text, parts[0])
			}
		}
		return os.WriteFile(target, []byte(text), 0o644)
	})
	if err != nil {
		return nil, err
	}

	agents, _ := filepath.Glob(filepath.Join(plugin, "agents", "*.md"))
	sort.Strings(agents)
	var names []string
	for _, f := range agents {
		if filepath.Base(f) == "README.md" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		front, body := splitFrontmatter(string(data))
		name := "tix-" + strings.TrimSuffix(filepath.Base(f), ".md")
		cfg := map[string]any{
			"name":        name,
			"description": KiroText(frontValue(front, "description")),
			"prompt":      kiroToolNotes + KiroText(strings.TrimLeft(body, "\n")),
			"tools":       []string{"*"},
			// kiro gives an agent without it no MCP servers, so the
			// subagents couldn't reach Jira/Bitbucket as the session can
			"includeMcpJson": true,
		}
		out, err := marshalIndent(cfg)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(agentsOut, name+".json"), out, 0o644); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

// kiroSkillFile names the skill name in its frontmatter and puts the tool
// notes after it.
func kiroSkillFile(text, name string) string {
	front, body := splitFrontmatter(text)
	if front == "" {
		return kiroToolNotes + text
	}
	var lines []string
	named := false
	for _, l := range strings.Split(front, "\n") {
		if strings.HasPrefix(l, "name:") {
			l, named = "name: "+name, true
		}
		lines = append(lines, l)
	}
	if !named {
		lines = append([]string{"name: " + name}, lines...)
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n\n" + kiroToolNotes + strings.TrimLeft(body, "\n")
}

// splitFrontmatter splits "---\n<front>\n---\n<body>"; no frontmatter: "", text.
func splitFrontmatter(text string) (string, string) {
	t := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(t, "---\n") {
		return "", text
	}
	end := strings.Index(t[4:], "\n---\n")
	if end < 0 {
		return "", text
	}
	return t[4 : 4+end], t[4+end+5:]
}

// frontValue is a one-line frontmatter value.
func frontValue(front, key string) string {
	for _, l := range strings.Split(front, "\n") {
		if v, ok := strings.CutPrefix(l, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func marshalIndent(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return []byte(b.String()), err
}
