package cli

import (
	"strings"
	"testing"
)

func TestStartPrompt(t *testing.T) {
	for _, c := range []struct {
		skill  string
		resume bool
		nudge  bool
	}{
		{"work-ticket", false, false},
		{"work-ticket", true, true},
		{"pr-feedback", true, false},
		{"pr-feedback", false, false},
	} {
		got := startPrompt(c.skill, "MAN-1", c.resume)
		if c.nudge {
			if !strings.HasPrefix(got, "ct start --continue:") || strings.Count(got, "/tickets:work-ticket MAN-1") != 2 {
				t.Errorf("%s resume=%v: not the nudge: %q", c.skill, c.resume, got)
			}
			if strings.ContainsAny(got, "`#;'\"") {
				t.Errorf("nudge has a character the shell would change: %q", got)
			}
			continue
		}
		if want := "/tickets:" + c.skill + " MAN-1"; got != want {
			t.Errorf("%s resume=%v: got %q, want %q", c.skill, c.resume, got, want)
		}
	}
}
