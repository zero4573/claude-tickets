package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/note"
)

// defaultContextWarn is where ct hook context warns, in tokens.
var defaultContextWarn = []int{150_000, 200_000, 250_000}

func hookContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Warn when the session's context gets large (UserPromptSubmit)",
		Long: `Reads the context size of the session from its transcript (the token usage
of the last reply) and, once per threshold, shows a warning and tells the
session; past the last threshold, on every prompt. A ticket session's PO is
told to run /tickets:po-refresh itself (it checkpoints, then prints the
command to type); any other session to suggest /compact. The thresholds are
CLAUDE_TICKETS_CONTEXT_WARN (tokens, comma separated; default
150000,200000,250000). Silent when the size can't be read; never fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out := contextHook(os.Stdin, contextThresholds(os.Getenv("CLAUDE_TICKETS_CONTEXT_WARN")), contextStateDir()); out != nil {
				_ = json.NewEncoder(os.Stdout).Encode(out)
			}
			return nil
		},
	}
}

func hookResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "After /clear or /compact, point a ticket session back at its board (SessionStart)",
		Long: `For SessionStart with source clear or compact in a ticket workspace: adds
the PO's resume step (step 0 of /tickets:work-ticket from board.md and
relay.md) to the session's context, so a cleared or compacted session
carries on without being told. After a compaction it also says the agents
are still running, so the PO never takes it for a new session (whose agents
would be gone). Each one is logged in <workspace>/.session-log with the
session id and the board's po: session: (CHANGED when they differ).
Elsewhere it does nothing; never fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out := resumeHook(os.Stdin); out != nil {
				_ = json.NewEncoder(os.Stdout).Encode(out)
			}
			return nil
		},
	}
}

// contextStateDir holds the last threshold each session was warned at.
func contextStateDir() string { return filepath.Join(config.CacheDir(), "context") }

// contextStateMaxAge is how long a session's state file is kept.
const contextStateMaxAge = 3 * 24 * time.Hour

// pruneContextState removes state files older than contextStateMaxAge.
func pruneContextState(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() && time.Since(info.ModTime()) > contextStateMaxAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// contextThresholds parses CLAUDE_TICKETS_CONTEXT_WARN, sorted; the default
// when it's empty or has no valid number.
func contextThresholds(v string) []int {
	var out []int
	for _, f := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return defaultContextWarn
	}
	sort.Ints(out)
	return out
}

type hookOutput struct {
	SystemMessage string             `json:"systemMessage,omitempty"`
	Specific      hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	Event   string `json:"hookEventName"`
	Context string `json:"additionalContext,omitempty"`
}

func contextHook(in io.Reader, thresholds []int, stateDir string) *hookOutput {
	var p struct {
		Session    string `json:"session_id"`
		Transcript string `json:"transcript_path"`
		Cwd        string `json:"cwd"`
	}
	if json.NewDecoder(in).Decode(&p) != nil || p.Transcript == "" || p.Session == "" ||
		strings.ContainsAny(p.Session, `/\`) {
		return nil
	}
	tokens, ok := contextTokens(p.Transcript)
	if !ok {
		return nil
	}
	level := 0
	for level < len(thresholds) && tokens >= thresholds[level] {
		level++
	}
	state := filepath.Join(stateDir, p.Session)
	last, _ := strconv.Atoi(strings.TrimSpace(readFile(state)))
	if os.MkdirAll(stateDir, 0o700) == nil {
		pruneContextState(stateDir)
		_ = os.WriteFile(state, []byte(strconv.Itoa(level)+"\n"), 0o644)
	}
	top := level == len(thresholds)
	if level == 0 || (level <= last && !top) {
		return nil
	}
	ticket := false
	if dir, info := findWorkspace(p.Cwd); dir != "" && info.Vault != "" && note.Find(info.Vault, info.ID) != "" {
		ticket = true
	}
	when := "at the next quiet point"
	if top {
		when = "now"
	}
	size := fmt.Sprintf("Context at %dk tokens (past %dk)", tokens/1000, thresholds[level-1]/1000)
	if ticket {
		msg := fmt.Sprintf("%s: the PO refreshes %s (/tickets:po-refresh).", size, when)
		return &hookOutput{SystemMessage: msg, Specific: hookSpecificOutput{Event: "UserPromptSubmit",
			Context: fmt.Sprintf("%s. Run /tickets:po-refresh yourself %s: it checkpoints board.md and relay.md, then prints the command for the user.", size, when)}}
	}
	msg := fmt.Sprintf("%s: run /compact %s.", size, when)
	return &hookOutput{SystemMessage: msg, Specific: hookSpecificOutput{Event: "UserPromptSubmit",
		Context: msg + " Mention it to the user at the end of this turn."}}
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// contextTokens is the context size after the last reply of the main
// conversation in a transcript: its input, cache-read and cache-creation
// tokens. Only the last 4 MiB are read.
func contextTokens(transcript string) (int, bool) {
	f, err := os.Open(transcript)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 4<<20 {
		_, _ = f.Seek(st.Size()-4<<20, io.SeekStart)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	tokens, ok := 0, false
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var e struct {
			Type      string `json:"type"`
			Sidechain bool   `json:"isSidechain"`
			Message   struct {
				Usage *struct {
					Input         int `json:"input_tokens"`
					CacheRead     int `json:"cache_read_input_tokens"`
					CacheCreation int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Sidechain || e.Message.Usage == nil {
			continue
		}
		u := e.Message.Usage
		tokens, ok = u.Input+u.CacheRead+u.CacheCreation, true
	}
	if sc.Err() != nil {
		return 0, false
	}
	return tokens, ok
}

const resumeCleared = "Ticket %s: you are its PO (/tickets:work-ticket %s). The conversation was just cleared: " +
	"redo step 0 (Resume) of /tickets:work-ticket %s from board.md and the Open part of relay.md, then carry on. " +
	"If that playbook is no longer in your context, run /tickets:work-ticket %s."

// resumeCompacted keeps step 0 from taking a compaction for a new session:
// the agents of this conversation survive it, whatever the session id says.
const resumeCompacted = "Ticket %s: you are its PO (/tickets:work-ticket %s). The conversation was just compacted: " +
	"it is the same conversation, and the lead and every agent in po: agents: are still running. " +
	"Redo step 0 (Resume) of /tickets:work-ticket %s from board.md and the Open part of relay.md, taking the same-session " +
	"path even if po: session: differs from $CLAUDE_CODE_SESSION_ID (then set it to that id; never mark tasks orphaned " +
	"or spawn a second lead), then carry on. If that playbook is no longer in your context, run /tickets:work-ticket %s."

func resumeHook(in io.Reader) *hookOutput {
	var p struct {
		Cwd     string `json:"cwd"`
		Source  string `json:"source"`
		Session string `json:"session_id"`
	}
	if json.NewDecoder(in).Decode(&p) != nil || (p.Source != "clear" && p.Source != "compact") {
		return nil
	}
	dir, info := findWorkspace(p.Cwd)
	if dir == "" || info.Vault == "" || note.Find(info.Vault, info.ID) == "" {
		return nil
	}
	board := boardSession(filepath.Join(note.TicketDir(info.Vault, info.ID), "board.md"))
	logSessionStart(dir, p.Source, p.Session, board)
	text := resumeCleared
	if p.Source == "compact" {
		text = resumeCompacted
	}
	id := info.ID
	return &hookOutput{Specific: hookSpecificOutput{Event: "SessionStart",
		Context: fmt.Sprintf(text, id, id, id, id)}}
}

// boardSession is a board.md's po: session: value ("" when there's none).
func boardSession(board string) string {
	f, err := os.Open(board)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inPO, fences := false, 0
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "---" {
			if fences++; fences == 2 {
				break
			}
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inPO = strings.HasPrefix(line, "po:")
			continue
		}
		if t := strings.TrimSpace(line); inPO && strings.HasPrefix(t, "session:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "session:")), `"'`)
		}
	}
	return ""
}

// logSessionStart appends one line per clear or compact to
// <workspace>/.session-log (the last 200 kept): when, which, the session id
// and the board's po: session:, so a compaction that changes the id shows.
func logSessionStart(dir, source, session, board string) {
	p := filepath.Join(dir, ".session-log")
	lines := strings.Split(strings.TrimRight(readFile(p), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	mark := ""
	if board != "" && session != board {
		mark = " CHANGED"
	}
	lines = append(lines, fmt.Sprintf("%s %s session=%s board=%s%s", time.Now().Format(time.RFC3339), source, session, board, mark))
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
