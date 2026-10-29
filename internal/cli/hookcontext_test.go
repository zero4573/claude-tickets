package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// transcript writes a transcript whose last main-conversation reply used
// tokens of context (a sidechain reply after it doesn't count).
func transcript(t *testing.T, dir string, tokens int) string {
	t.Helper()
	p := filepath.Join(dir, "t.jsonl")
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","isSidechain":false,"message":{"usage":{"input_tokens":1,"cache_read_input_tokens":10,"cache_creation_input_tokens":0}}}`,
		fmt.Sprintf(`{"type":"assistant","isSidechain":false,"message":{"usage":{"input_tokens":2,"cache_read_input_tokens":%d,"cache_creation_input_tokens":1000}}}`, tokens-1002),
		`{"type":"assistant","isSidechain":true,"message":{"usage":{"input_tokens":5,"cache_read_input_tokens":999999,"cache_creation_input_tokens":0}}}`,
		`not json`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestContextHook(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	th := []int{150_000, 200_000, 250_000}
	run := func(tokens int) string {
		tr := transcript(t, dir, tokens)
		in := fmt.Sprintf(`{"session_id":"s1","transcript_path":%q,"cwd":%q}`, tr, dir)
		out := contextHook(strings.NewReader(in), th, state)
		if out == nil {
			return ""
		}
		if out.Specific.Event != "UserPromptSubmit" || !strings.HasPrefix(out.Specific.Context, out.SystemMessage) ||
			!strings.Contains(out.Specific.Context, "Mention it to the user") {
			t.Errorf("%d: %+v", tokens, out)
		}
		return out.SystemMessage
	}
	for _, c := range []struct {
		tokens int
		want   string // "" for no warning
	}{
		{100_000, ""},
		{160_000, "Context at 160k tokens (past 150k): run /compact at the next quiet point."},
		{170_000, ""}, // warned at 150k already
		{210_000, "past 200k"},
		{260_000, "Context at 260k tokens (past 250k): run /compact now."},
		{270_000, "past 250k"}, // past the last: every prompt
		{90_000, ""},           // compacted: start over
		{155_000, "past 150k"},
	} {
		got := run(c.tokens)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%d tokens: got %q, want %q", c.tokens, got, c.want)
		}
	}
	if contextHook(strings.NewReader(`{"session_id":"s1","transcript_path":"/nope"}`), th, state) != nil {
		t.Error("unreadable transcript: silent")
	}
	if contextHook(strings.NewReader(`{"session_id":"../x","transcript_path":"/nope"}`), th, state) != nil {
		t.Error("session id with a separator: silent")
	}
}

func TestContextHookTicket(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault")
	ws := filepath.Join(dir, "work", "MAN-1")
	for _, d := range []string{filepath.Join(vault, "tickets", "manual", "MAN-1"), filepath.Join(ws, "repo")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(vault, "tickets", "manual", "MAN-1", "MAN-1.md"), "---\ntype: ticket\n---\n")
	write(filepath.Join(ws, "workspace.json"), fmt.Sprintf(`{"id":"MAN-1","vault":%q,"repos":[]}`, vault))
	tr := transcript(t, dir, 160_000)
	in := fmt.Sprintf(`{"session_id":"s2","transcript_path":%q,"cwd":%q}`, tr, filepath.Join(ws, "repo"))
	out := contextHook(strings.NewReader(in), defaultContextWarn, filepath.Join(dir, "state"))
	if out == nil || out.SystemMessage != "Context at 160k tokens (past 150k): the PO refreshes at the next quiet point (/tickets:po-refresh)." ||
		!strings.Contains(out.Specific.Context, "Run /tickets:po-refresh yourself at the next quiet point") {
		t.Fatalf("ticket session: %+v", out)
	}

	write(filepath.Join(vault, "tickets", "manual", "MAN-1", "board.md"),
		"---\ntype: board\nlead: a1\npo:\n  session: \"s-old\"\n  agents: []\n---\n# Board\n")
	for src, want := range map[string]string{"compact": "just compacted", "clear": "just cleared", "startup": "", "resume": ""} {
		out := resumeHook(strings.NewReader(fmt.Sprintf(`{"cwd":%q,"source":%q,"session_id":"s-new"}`, ws, src)))
		switch {
		case want == "" && out != nil:
			t.Errorf("%s: %+v", src, out)
		case want != "" && (out == nil || out.Specific.Event != "SessionStart" || !strings.Contains(out.Specific.Context, want) ||
			!strings.Contains(out.Specific.Context, "/tickets:work-ticket MAN-1")):
			t.Errorf("%s: %+v", src, out)
		}
	}
	// a compaction is never a new session: the agents run on
	out = resumeHook(strings.NewReader(fmt.Sprintf(`{"cwd":%q,"source":"compact","session_id":"s-new"}`, ws)))
	for _, s := range []string{"still running", "same-session path even if po: session: differs", "never mark tasks orphaned or spawn a second lead"} {
		if !strings.Contains(out.Specific.Context, s) {
			t.Errorf("compact: missing %q", s)
		}
	}
	// each clear or compact is logged, CHANGED when the id isn't the board's
	log := readFile(filepath.Join(ws, ".session-log"))
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 3 || !strings.Contains(log, " compact session=s-new board=s-old CHANGED\n") ||
		!strings.Contains(log, " clear session=s-new board=s-old CHANGED\n") {
		t.Errorf("session log:\n%s", log)
	}
	write(filepath.Join(vault, "tickets", "manual", "MAN-1", "board.md"), "---\npo:\n  session: s-new\n---\n")
	resumeHook(strings.NewReader(fmt.Sprintf(`{"cwd":%q,"source":"compact","session_id":"s-new"}`, ws)))
	if l := readFile(filepath.Join(ws, ".session-log")); !strings.HasSuffix(l, "compact session=s-new board=s-new\n") {
		t.Errorf("same id: %q", l)
	}
	if resumeHook(strings.NewReader(fmt.Sprintf(`{"cwd":%q,"source":"clear"}`, dir))) != nil {
		t.Error("outside a ticket workspace: nothing")
	}
}

func TestBoardSession(t *testing.T) {
	f := filepath.Join(t.TempDir(), "board.md")
	for board, want := range map[string]string{
		"---\nlead: a1\npo:\n  session: \"abc\"\n  agents: []\n---\n": "abc",
		"---\npo:\n  session: abc\n---\n":                             "abc",
		"---\nlead:\n  session: x\npo:\n  agents: []\n---\n":          "",
		"---\ntype: board\n---\npo:\n  session: x\n":                  "",
	} {
		if err := os.WriteFile(f, []byte(board), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := boardSession(f); got != want {
			t.Errorf("%q: got %q", board, got)
		}
	}
}

func TestContextThresholds(t *testing.T) {
	if got := contextThresholds(""); fmt.Sprint(got) != "[150000 200000 250000]" {
		t.Error(got)
	}
	if got := contextThresholds("300000, 100000,x"); fmt.Sprint(got) != "[100000 300000]" {
		t.Error(got)
	}
}

func TestContextStateDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	t.Setenv("HOME", "/x/home")
	d := contextStateDir()
	if strings.HasPrefix(d, os.TempDir()) || filepath.Base(d) != "context" {
		t.Errorf("contextStateDir = %q", d)
	}
}

func TestPruneContextState(t *testing.T) {
	dir := t.TempDir()
	old, fresh := filepath.Join(dir, "old"), filepath.Join(dir, "fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	pruneContextState(dir)
	if _, err := os.Stat(old); err == nil {
		t.Error("old state file kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh state file removed")
	}
}

func TestContextTokensOverlongLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	usage := `{"type":"assistant","isSidechain":false,"message":{"usage":{"input_tokens":1,"cache_read_input_tokens":300000,"cache_creation_input_tokens":0}}}`
	body := usage + "\n" + strings.Repeat("x", 17<<20) + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, ok := contextTokens(p); ok {
		t.Errorf("contextTokens = %d, true; want no result after an over-long line", n)
	}
}
