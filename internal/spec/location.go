package spec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zero4573/claude-tickets/internal/note"
)

// Where a repo's specs live.
const (
	InRepo  = "repo"
	InVault = "vault"
	Unset   = ""
)

// Location says where a repo's specs are. Kind is InRepo, InVault, or
// Unset (neither the project note's specs: field nor a specs/ in the
// repo: the caller asks the user once). Dir is the specs root, empty when
// Unset. Field is the raw specs: value of the project note.
type Location struct {
	Kind  string `json:"kind"`
	Dir   string `json:"dir"`
	Field string `json:"field"`
}

// ProjectNote is the path of a project's index note.
func ProjectNote(vault, slug string) string {
	return filepath.Join(vault, "projects", slug, slug+".md")
}

// VaultDir is where a vault-side spec lives: projects/<slug>/specs.
func VaultDir(vault, slug string) string {
	return filepath.Join(vault, "projects", slug, "specs")
}

// Resolve finds where a repo's specs live. The project note's specs:
// field (repo or vault) decides; without it, a specs/ directory in the
// repo means repo; otherwise Unset. Any other field value is an error.
func Resolve(vault, slug, repoDir string) (Location, error) {
	field := note.Get(ProjectNote(vault, slug), "specs")
	switch field {
	case InRepo:
		return Location{Kind: InRepo, Dir: filepath.Join(repoDir, "specs"), Field: field}, nil
	case InVault:
		return Location{Kind: InVault, Dir: VaultDir(vault, slug), Field: field}, nil
	case "":
		if st, err := os.Stat(filepath.Join(repoDir, "specs")); err == nil && st.IsDir() {
			return Location{Kind: InRepo, Dir: filepath.Join(repoDir, "specs")}, nil
		}
		return Location{}, nil
	}
	return Location{}, fmt.Errorf("%s: specs must be repo or vault, got %q", ProjectNote(vault, slug), field)
}
