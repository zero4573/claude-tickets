package cli

import (
	"path/filepath"

	"github.com/zero4573/claude-tickets/internal/vindex"
)

// Freshness: the notes ct writes go through vindex.Refresh, which does
// nothing without an index and reports its own errors without failing the
// command.
func init() { afterNoteWrite = func(vault, rel string) { refreshIndex(vault, rel) } }

// refreshIndex is vindex.Refresh; tests swap it to record the paths a
// command passes (none: the whole vault).
var refreshIndex = vindex.Refresh

// relTo is path relative to vault ("/"-separated), or path as given.
func relTo(vault, path string) string {
	if r, err := filepath.Rel(vault, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}
