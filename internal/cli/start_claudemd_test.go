package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/vault"
)

func TestTicketClaudeMDCommitRules(t *testing.T) {
	tests := []struct {
		name, source, board string
		want, wantNot       []string
	}{
		{"synced", "jira", "commit-style: ticket\n",
			[]string{"subject starting with `K-1`, unless", "normally on `feature/K-1[-<description>]`", "never\n  go into code"},
			[]string{"subjects never carry"}},
		{"manual conventional", "manual", "commit-style: conventional\n",
			[]string{"subjects never carry the ticket ID", "style: conventional commits", "normally on `feature/<description>`"},
			[]string{"starting with `K-1`", "feature/K-1"}},
		{"manual repo", "manual", "commit-style: repo\n",
			[]string{"style: the repo's own convention, without an ID"},
			[]string{"starting with `K-1`"}},
		{"manual unset", "manual", "",
			[]string{"not chosen yet (the kickoff asks): a plain imperative summary"},
			[]string{"starting with `K-1`"}},
		{"manual ticket style", "manual", "commit-style: ticket\n",
			[]string{"not chosen yet (the kickoff asks)"},
			[]string{"starting with `K-1`"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := t.TempDir()
			dir := filepath.Join(v, "tickets", "K-1")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			write := func(name, body string) {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("K-1.md", "---\nsource: "+tc.source+"\n---\n# K-1\n")
			if tc.board != "" {
				write("board.md", "---\n"+tc.board+"---\n")
			}
			got := ticketClaudeMD(vault.Context{Vault: v}, "K-1", filepath.Join(v, "ws"))
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tc.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q", w)
				}
			}
			if !strings.Contains(got, "Vault-only IDs") {
				t.Error("missing vault-only sentence")
			}
		})
	}
}
