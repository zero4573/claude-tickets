package launcher

import (
	"errors"
	"slices"
	"testing"
)

func TestShellJoin(t *testing.T) {
	got := ShellJoin([]string{"env", "A=b c", "claude", "--x", "it's", ""})
	if want := `env 'A=b c' claude --x 'it'\''s' ''`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNone(t *testing.T) {
	var l Launcher = none{}
	if l.Caps() != (Caps{}) {
		t.Errorf("none can't do anything in the background: %+v", l.Caps())
	}
	if l.Windows("g") != nil || HasWindow(l, "g", "w") {
		t.Error("none has no windows")
	}
	for name, err := range map[string]error{
		"Open":     l.Open(Window{}),
		"SendKeys": l.SendKeys("g", "w", "x"),
		"Attach":   l.Attach("g", "w"),
	} {
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("none.%s: %v, want ErrUnsupported", name, err)
		}
	}
}

func TestTmuxCaps(t *testing.T) {
	if got, want := (Tmux{}).Caps(), (Caps{Background: true, List: true, SendKeys: true, Attach: true}); got != want {
		t.Errorf("tmux caps %+v, want %+v", got, want)
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"A=1", "CLAUDE_TICKETS_VAULT=/old", "B=2=3"}, map[string]string{"CLAUDE_TICKETS_VAULT": "/v", "C": "4"})
	slices.Sort(got)
	want := []string{"A=1", "B=2=3", "C=4", "CLAUDE_TICKETS_VAULT=/v"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %q, want %q", got, want)
	}
}
