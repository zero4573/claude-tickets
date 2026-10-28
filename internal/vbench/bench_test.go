package vbench

import (
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/zero4573/claude-tickets/internal/vsearch"
)

func TestSearchOptions(t *testing.T) {
	for _, c := range []struct {
		name string
		f    Fact
		want vsearch.Options
	}{
		{"kb", Fact{Scenario: "kb", Project: "p"}, vsearch.Options{K: 30, Budget: Budget}},
		{"recall project", Fact{Scenario: "recall", Project: "p"}, vsearch.Options{K: 30, Budget: RecallBudget, Project: "p"}},
		{"recall ticket", Fact{Scenario: "recall", Project: "p", Ticket: "T-1"},
			vsearch.Options{K: 30, Budget: RecallBudget, Paths: []string{"tickets/jira/T-1/"}, Type: "board"}},
	} {
		if got := SearchOptions(c.f); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The recall skill's step 0 commands search with the budget the bench measures.
func TestRecallSkillBudget(t *testing.T) {
	data, err := os.ReadFile("../../assets/plugin/skills/recall/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`ct vault search[^\x60]*--budget (\d+)`).FindAllStringSubmatch(string(data), -1)
	if len(m) != 2 {
		t.Fatalf("want 2 step-0 searches (project, ticket), found %d", len(m))
	}
	for _, s := range m {
		if n, _ := strconv.Atoi(s[1]); n != RecallBudget {
			t.Errorf("recall skill uses --budget %d, vbench.RecallBudget is %d", n, RecallBudget)
		}
	}
}
