package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIntervalFlag(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"1s": time.Second, "2s": 2 * time.Second, "1m": time.Minute, "1.5s": 1500 * time.Millisecond,
	} {
		var f intervalFlag
		if err := f.Set(in); err != nil {
			t.Errorf("Set(%q): %v", in, err)
		} else if time.Duration(f) != want {
			t.Errorf("Set(%q) = %v, want %v", in, time.Duration(f), want)
		}
	}
	for _, in := range []string{"500ms", "999ms", "0", "0s", "-1s", "abc", "2", ""} {
		f := intervalFlag(2 * time.Second)
		err := f.Set(in)
		if err == nil {
			t.Errorf("Set(%q) accepted", in)
			continue
		}
		if !strings.Contains(err.Error(), "at least 1s") {
			t.Errorf("Set(%q): %q doesn't name the minimum", in, err)
		}
		if time.Duration(f) != 2*time.Second {
			t.Errorf("Set(%q) changed the value to %v", in, time.Duration(f))
		}
	}
	if f := intervalFlag(2 * time.Second); f.String() != "2s" || f.Type() != "duration" {
		t.Errorf("String/Type = %q/%q", f.String(), f.Type())
	}
}

// writeStatus renders what ct status always printed: the header, then a
// table padded by two spaces, or the empty line.
func TestWriteStatus(t *testing.T) {
	var b bytes.Buffer
	err := writeStatus(&b, statusView{
		header: "Vault a, tmux session tickets-a",
		rows: []statusRow{
			{0, "MAN-1", []string{"MAN-1", "manual", "needs-input", "yes", "new", "2 (1 dirty)", "10:00:00", "Which base branch?"}},
			{3, "MAN-22", []string{"MAN-22", "jira", "-", "no", "in-progress", "0 (0 dirty)", "", ""}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Vault a, tmux session tickets-a\n" +
		"TICKET  SOURCE  AGENT        WINDOW  STATUS       REPOS        SINCE     WAITING ON\n" +
		"MAN-1   manual  needs-input  yes     new          2 (1 dirty)  10:00:00  Which base branch?\n" +
		"MAN-22  jira    -            no      in-progress  0 (0 dirty)            \n"
	if got := b.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	b.Reset()
	if err := writeStatus(&b, statusView{header: "Vault a, no terminal multiplexer (sessions run in their own terminal)", empty: "No ticket workspaces under /w (start one with ct start <ID>)."}); err != nil {
		t.Fatal(err)
	}
	want = "Vault a, no terminal multiplexer (sessions run in their own terminal)\nNo ticket workspaces under /w (start one with ct start <ID>).\n"
	if got := b.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLineCollector(t *testing.T) {
	var c lineCollector
	c.Write([]byte("ct: a: ignoring x\nct: a: ignoring x\n"))
	c.Write([]byte("ct: b: ignoring y\n"))
	if got, want := c.drain(), []string{"ct: a: ignoring x", "ct: b: ignoring y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := c.drain(); got != nil {
		t.Errorf("drained twice: %q", got)
	}
}
