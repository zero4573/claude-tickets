package cli

import (
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/harness"
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
		h, _ := harness.Get(harness.Claude)
		got := startPrompt(h, c.skill, "MAN-1", c.resume)
		if c.nudge {
			if !strings.HasPrefix(got, "tix start --continue:") || strings.Count(got, "/tix:work-ticket MAN-1") != 2 {
				t.Errorf("%s resume=%v: not the nudge: %q", c.skill, c.resume, got)
			}
			if strings.ContainsAny(got, "`#;'\"") {
				t.Errorf("nudge has a character the shell would change: %q", got)
			}
			continue
		}
		if want := "/tix:" + c.skill + " MAN-1"; got != want {
			t.Errorf("%s resume=%v: got %q, want %q", c.skill, c.resume, got, want)
		}
	}
}

// kiro sessions name the skills as kiro does: /tix-<skill>
func TestStartPromptKiro(t *testing.T) {
	h, _ := harness.Get(harness.Kiro)
	if got := startPrompt(h, "work-ticket", "MAN-1", false); got != "/tix-work-ticket MAN-1" {
		t.Errorf("new: %q", got)
	}
	if got := startPrompt(h, "work-ticket", "MAN-1", true); strings.Count(got, "/tix-work-ticket MAN-1") != 2 || strings.Contains(got, "tix:") {
		t.Errorf("continued: %q", got)
	}
}
