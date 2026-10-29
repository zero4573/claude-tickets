package assets

import (
	"strings"
	"testing"
)

func TestFixLoopLeadStopRule(t *testing.T) {
	s := retroText(t, "plugin/skills/lead/SKILL.md")
	for _, w := range []string{
		"**Fix loops:**",
		"previous fix range",
		"two fixes undoing each other",
		"gets no fix task",
		"`plan.md ## Decisions`",
		"design option (recommended first)",
		"`reference.md`, \"Fix loops\"",
	} {
		if !contains(s, w) {
			t.Errorf("lead: missing %q", w)
		}
	}
}

func TestFixLoopReference(t *testing.T) {
	s := retroText(t, "plugin/skills/lead/reference.md")
	for _, w := range []string{
		"## Fix loops",
		"Stop patching in an area",
		"regression of the last fix round",
		"the same area was fixed in consecutive rounds",
		"Then write no fix task for that finding",
		"ask the user through the gate",
		"the recommended design first",
		"fixed as usual",
	} {
		if !contains(s, w) {
			t.Errorf("reference: missing %q", w)
		}
	}
}

func TestFixLoopReviewerBehaviourRegression(t *testing.T) {
	s := retroText(t, "plugin/skills/wave-review/SKILL.md")
	for _, w := range []string{
		"Judge by behaviour, not only by line age",
		"recorded user decision secured",
		"even when its lines are new",
		"Name the earlier ID and what it secured",
		"Tag `new` only when no earlier finding or decision covered that behaviour",
	} {
		if !contains(s, w) {
			t.Errorf("wave-review: missing %q", w)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
