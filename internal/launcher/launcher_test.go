package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"A=1", "CLAUDE_TICKETS_VAULT=/old", "B=2=3"}, map[string]string{"CLAUDE_TICKETS_VAULT": "/v", "C": "4"})
	slices.Sort(got)
	want := []string{"A=1", "B=2=3", "C=4", "CLAUDE_TICKETS_VAULT=/v"}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %q, want %q", got, want)
	}
}

func TestWithPathPrependsToTheLaunchPath(t *testing.T) {
	env := map[string]string{"A": "1"}
	if got := withPath(env, "", "/usr/bin"); got["PATH"] != "" || len(got) != 1 {
		t.Errorf("no PathFirst: %v", got)
	}
	got := withPath(env, filepath.Join("ws", "bin"), "/usr/bin")
	if want := filepath.Join("ws", "bin") + string(os.PathListSeparator) + "/usr/bin"; got["PATH"] != want || got["A"] != "1" {
		t.Errorf("withPath = %v, want PATH %s", got, want)
	}
	if _, ok := env["PATH"]; ok {
		t.Error("the session's own Env was changed")
	}
	if got := withPath(nil, "bin", ""); got["PATH"] != "bin" {
		t.Errorf("an empty PATH: %v", got)
	}
}
