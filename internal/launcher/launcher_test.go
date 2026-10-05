package launcher

import (
	"errors"
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
	if l.Windows("g") != nil || HasWindow(l, "g", "w") {
		t.Error("none has no windows")
	}
	if !errors.Is(l.Open(Window{}), ErrNoBackend) || !errors.Is(l.Attach("g", "w"), ErrNoBackend) {
		t.Error("none refuses to open or attach")
	}
}
