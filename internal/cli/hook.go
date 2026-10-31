package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/platform"
	"github.com/zero4573/tix/internal/workspace"
)

func hookCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "The hooks of tix sessions (Claude Code's plugin, kiro's agent file)",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "agent-state needs-input|working|idle|exited",
		Short: "Record what a session is doing, for tix status",
		Long: `Records what a ticket or spec session is doing in <workspace>/.agent-state,
for tix status, with the hook's JSON payload on stdin. The workspace is the
nearest folder at or above the session's cwd whose workspace.json has an
id; outside one this does nothing. Never fails: a hook error would only get
in the session's way.

It also desktop-notifies (notify-send on Linux, Notification Center on
macOS) when the session needs you: an AskUserQuestion (which raises no
Notification hook of its own), a permission or elicitation prompt, or the
session idle and waiting for you (Notification idle_prompt; not while the
ticket's board.md lists running agents, whose notices will wake it). Once:
no other ping until the next prompt or answer (working). The notification
stays until dismissed where the daemon allows it. TIX_NOTIFY=0
turns them off.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentState(args[0], os.Stdin, agent)
			return nil
		},
	})
	// kiro's hooks run these with --agent kiro (its agent file, written by
	// tix's kiro harness): the same JSON, tool_input sent as a string
	cmd.PersistentFlags().StringVar(&agent, "agent", "claude", "the agent CLI whose hook this is (claude or kiro)")
	cmd.AddCommand(hookContextCmd(), hookResumeCmd())
	return cmd
}

// hookPayload is what the hooks read of their JSON input. tool_input is an
// object from Claude Code, a JSON string from kiro: decoded either way.
type hookPayload struct {
	Cwd       string          `json:"cwd"`
	Event     string          `json:"hook_event_name"`
	Message   string          `json:"message"`
	Type      string          `json:"notification_type"`
	RawInput  json.RawMessage `json:"tool_input"`
	ToolInput struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	} `json:"-"`
}

func parseHook(data []byte) (hookPayload, error) {
	var p hookPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	raw := p.RawInput
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = []byte(s)
	}
	_ = json.Unmarshal(raw, &p.ToolInput)
	return p, nil
}

// agentLabel is the CLI's name in notifications.
func agentLabel(agent string) string {
	if agent == "kiro" {
		return "kiro"
	}
	return "Claude Code"
}

func agentState(state string, in io.Reader, agent string) {
	data, _ := io.ReadAll(in)
	p, err := parseHook(data)
	if err != nil || p.Cwd == "" {
		return
	}
	dir, info := findWorkspace(p.Cwd)
	if dir == "" {
		return
	}
	notified := filepath.Join(dir, ".notified")
	// working comes after every tool call: when nothing changes (already
	// working, no ping to re-arm), leave the file alone
	if state == "working" && workspace.ReadState(dir).State == "working" {
		if _, err := os.Lstat(notified); errors.Is(err, fs.ErrNotExist) {
			return
		}
	}
	reason := ""
	if state == "needs-input" {
		switch {
		case len(p.ToolInput.Questions) > 0 && p.ToolInput.Questions[0].Question != "":
			reason = p.ToolInput.Questions[0].Question
		case p.Message != "":
			reason = p.Message
		default:
			reason = "needs input"
		}
	}
	out, _ := json.MarshalIndent(workspace.State{State: state, Reason: reason, TS: time.Now().Format(time.RFC3339)}, "", "  ")
	tmp := filepath.Join(dir, ".agent-state.tmp")
	if os.WriteFile(tmp, append(out, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(dir, ".agent-state"))
	}
	switch {
	case state == "working":
		_ = os.Remove(notified)
	case state == "needs-input" && p.Event == "PreToolUse":
		ping(notified, agentLabel(agent)+" ("+info.ID+"): question", reason)
	case state == "needs-input" && p.Event == "Notification":
		ping(notified, agentLabel(agent)+" ("+info.ID+"): needs you", reason)
	case p.Event == "Notification" && p.Type == "idle_prompt":
		if info.Vault != "" && runningAgents(filepath.Join(note.TicketDir(info.Vault, info.ID), "board.md")) {
			return
		}
		msg := p.Message
		if msg == "" {
			msg = "waiting for your reply"
		}
		ping(notified, agentLabel(agent)+" ("+info.ID+"): waiting for you", msg)
	}
}

// findWorkspace is the nearest folder at or above cwd whose workspace.json
// has an id, and that file ("" when there's none).
func findWorkspace(cwd string) (string, workspace.Info) {
	for d := cwd; d != "" && d != "." && filepath.Dir(d) != d; d = filepath.Dir(d) {
		if i, err := workspace.Read(d); err == nil && i.ID != "" {
			return d, i
		}
	}
	return "", workspace.Info{}
}

// ping notifies once until the flag file is removed (the next working).
func ping(flag, title, body string) {
	if os.Getenv("TIX_NOTIFY") == "0" {
		return
	}
	if _, err := os.Stat(flag); err == nil {
		return
	}
	_ = os.WriteFile(flag, nil, 0o644)
	platform.Notify("Claude Code", title, body, platform.Sticky)
}

// runningAgents reports whether a board.md's po: agents: lists any agent,
// as `agents: [a, b]` or as an indented list under `agents:`.
func runningAgents(board string) bool {
	f, err := os.Open(board)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inPO, inAgents, fences := false, false, 0
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "---" {
			if fences++; fences == 2 {
				return false
			}
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		switch {
		case indent == 0:
			inPO, inAgents = strings.HasPrefix(line, "po:"), false
		case inPO && strings.HasPrefix(trimmed, "agents:"):
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "agents:"))
			if v != "" {
				return v != "[]"
			}
			inAgents = true
		case inAgents && strings.HasPrefix(trimmed, "- "):
			return true
		case inAgents:
			inAgents = false
		}
	}
	return false
}
