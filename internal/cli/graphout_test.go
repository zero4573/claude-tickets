package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/zero4573/claude-tickets/internal/vault"
)

func TestGraphOutDirsCovered(t *testing.T) {
	root := t.TempDir()
	projects, work := filepath.Join(root, "p"), filepath.Join(root, "w")
	want := []string{
		filepath.Join(projects, "bb", "o", "r", "graphify-out"),
		filepath.Join(work, "MAN-1", "slug", "graphify-out"),
		filepath.Join(work, ".spec-x", "slug", "graphify-out"),
		filepath.Join(work, "MAN-1", ".cache", "graphify", "slug", "graphify-out"),
	}
	skipped := []string{
		filepath.Join(work, ".hidden", "slug", "graphify-out"),
		filepath.Join(work, "MAN-1", ".cache", "graphify", "slug", "other"),
	}
	for _, d := range append(append([]string{}, want...), skipped...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := graphOutDirs(vault.Context{Locations: vault.Locations{ProjectsRoot: projects, WorkRoot: work}})
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graphOutDirs = %v, want %v", got, want)
	}
}
