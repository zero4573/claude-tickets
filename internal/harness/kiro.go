package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/tix/assets"
	"github.com/zero4573/tix/internal/session"
)

// kiro is kiro-cli (v3 engine): the session's agent is
// <workspace>/.kiro/agents/tix.json, with the plugin rendered next to it
// (RenderKiro). Its command is $TIX_KIRO (split on spaces), else kiro-cli.
//
// What kiro can't be told, and why it is still covered:
//   - it has no path deny rules that hold (its permission rules don't stop
//     a session trusted with every tool): read-only folders come from
//     SANDBOX_RO_PATHS, which a sandbox mounts read-only
//   - it has no --add-dir: the folders are named in the agent's prompt and
//     given as SANDBOX_ADD_DIRS
//   - shell commands a session must not run are rules in its prompt
type kiro struct{}

func (kiro) Name() string                 { return Kiro }
func (kiro) SkillRef(skill string) string { return KiroSkill(skill) }

// AgentName is the session's kiro agent, <workspace>/.kiro/agents/tix.json.
const AgentName = "tix"

func (k kiro) Prepare(s Session) error {
	plugin := s.Plugin
	if plugin == "" {
		var err error
		if plugin, err = session.PluginDir(); err != nil {
			return err
		}
	}
	dotKiro := filepath.Join(s.Dir, ".kiro")
	agents, err := RenderKiro(plugin, dotKiro)
	if err != nil {
		return err
	}
	if s.Instructions != "" {
		if err := os.WriteFile(filepath.Join(s.Dir, "AGENTS.md"), []byte(KiroText(s.Instructions)), 0o644); err != nil {
			return err
		}
	}
	if s.Settings {
		if err := session.WriteIgnore(s.Dir); err != nil {
			return err
		}
	}
	exe := s.Hooks
	if exe == "" {
		if exe, err = assets.Executable(); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dotKiro, "agents", AgentName+".json"), kiroAgent(s, exe, agents), 0o644)
}

// kiroAgent is the session's agent file: its prompt (the folders, the
// rules, the system prompt), the instructions file as a resource, the MCP
// servers kiro's own config has, and the hooks that tell tix status what
// the session is doing.
func kiroAgent(s Session, exe string, subagents []string) []byte {
	var p strings.Builder
	p.WriteString("You are a tix session (the ticket workflow, with an Obsidian vault as long-term memory).\n")
	if s.Instructions != "" {
		p.WriteString("This workspace's instructions are in `AGENTS.md`: read them first and follow them.\n")
	}
	if dirs := append(append([]string{}, s.Dirs...), s.ReadOnly...); len(dirs) > 0 || s.ReposRoot != "" {
		p.WriteString("\nFolders this session works in besides the workspace:\n")
		for _, d := range s.Dirs {
			fmt.Fprintf(&p, "- `%s` (read-write)\n", d)
		}
		if s.ReposRoot != "" {
			fmt.Fprintf(&p, "- `%s` (the main clones: read-only, never edit, commit or push there)\n", s.ReposRoot)
		}
		for _, d := range s.ReadOnly {
			fmt.Fprintf(&p, "- `%s` (read-only)\n", d)
		}
	}
	if len(s.DenyShell) > 0 {
		p.WriteString("\nNever run these commands: `" + strings.Join(s.DenyShell, "`, `") + "`.\n")
	}
	if len(subagents) > 0 {
		p.WriteString("\nThe workflow's agents are your subagent tools: subagent_" + strings.Join(subagents, ", subagent_") + ".\n")
	}
	if s.SystemPrompt != "" {
		p.WriteString("\n" + KiroText(s.SystemPrompt) + "\n")
	}
	hook := func(args string) []map[string]any {
		return []map[string]any{{"command": fmt.Sprintf("%q hook --agent kiro %s", exe, args)}}
	}
	cfg := map[string]any{
		"name":           AgentName,
		"description":    "tix session",
		"prompt":         p.String(),
		"model":          KiroSessionModel, // the start: /model changes it for the session
		"tools":          []string{"*"},
		"includeMcpJson": true,
		"hooks": map[string]any{
			"userPromptSubmit": hook("agent-state working"),
			"postToolUse":      []map[string]any{{"matcher": "*", "command": hook("agent-state working")[0]["command"]}},
			"stop":             hook("agent-state idle"),
		},
	}
	if s.Instructions != "" {
		cfg["resources"] = []string{"file://AGENTS.md"}
	}
	out, _ := marshalIndent(cfg)
	return out
}

func (h kiro) Argv(s Session) ([]string, error) {
	argv := strings.Fields(os.Getenv("TIX_KIRO"))
	if len(argv) == 0 {
		argv = []string{"kiro-cli"}
	}
	argv = append(argv, "chat", "--agent", AgentName)
	if s.Continue {
		argv = append(argv, "--resume")
	}
	if s.Print {
		argv = append(argv, "--no-interactive")
	}
	argv = append(argv, s.Extra...)
	if p := FirstPrompt(h, s); p != "" {
		argv = append(argv, p)
	}
	return argv, nil
}

// Env: the folders as SANDBOX_*, and the workspace's Go caches (claude
// gets these from its settings)
func (kiro) Env(s Session) map[string]string {
	env := sandboxEnv(Session{Dirs: existing(s.Dirs), ReadOnly: existing(s.ReadOnly), ReposRoot: s.ReposRoot})
	if s.Settings {
		for k, v := range session.GoEnv(s.Dir) {
			env[k] = v
		}
	}
	return env
}
