package cli

import (
	"strings"
	"testing"
)

func TestManualSubjectRuleStyles(t *testing.T) {
	if r := manualSubjectRule("conventional"); !strings.Contains(r, "conventional commits") || strings.Contains(r, "not chosen yet") {
		t.Errorf("conventional: %q", r)
	}
	if r := manualSubjectRule(""); !strings.Contains(r, "not chosen yet") {
		t.Errorf("unset: %q", r)
	}
}
