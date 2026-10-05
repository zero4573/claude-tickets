package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/platform"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Claude Code hooks of the tickets plugin",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "agent-state needs-input|working|idle|exited",
		Short: "Record what a session is doing, for ct status",
		Long: `Records what a ticket or kb session is doing in <workspace>/.agent-state,
for ct status, with the hook's JSON payload on stdin. The workspace is the
nearest folder at or above the session's cwd whose workspace.json has an
id; outside one this does nothing. needs-input from an AskUserQuestion also
desktop-notifies (notify-send on Linux, Notification Center on macOS), since a question doesn't raise
a Notification hook of its own. Never fails: a hook error would only get
in the session's way.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentState(args[0], os.Stdin)
			return nil
		},
	})
	return cmd
}

func agentState(state string, in io.Reader) {
	var p struct {
		Cwd       string `json:"cwd"`
		Event     string `json:"hook_event_name"`
		Message   string `json:"message"`
		ToolInput struct {
			Questions []struct {
				Question string `json:"question"`
			} `json:"questions"`
		} `json:"tool_input"`
	}
	data, _ := io.ReadAll(in)
	if json.Unmarshal(data, &p) != nil || p.Cwd == "" {
		return
	}
	var dir string
	var info workspace.Info
	for d := p.Cwd; d != "" && d != "." && filepath.Dir(d) != d; d = filepath.Dir(d) {
		if i, err := workspace.Read(d); err == nil && i.ID != "" {
			dir, info = d, i
			break
		}
	}
	if dir == "" {
		return
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
	if state == "needs-input" && p.Event == "PreToolUse" {
		platform.Notify("Claude Code", "Claude Code ("+info.ID+"): question", reason, false)
	}
}
