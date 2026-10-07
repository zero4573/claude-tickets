package workspace

import (
	"os"
	"path/filepath"
	"time"
)

// stamp is what a stat of a workspace's .agent-state says, enough to tell
// that the hooks wrote it since: they write .agent-state.tmp and rename it
// over .agent-state, so a write gives a new file (and usually a new mtime
// and size).
type stamp struct {
	exists bool
	mod    time.Time
	size   int64
	fi     os.FileInfo
}

// Stamps is the .agent-state stamp of every ticket workspace under a work
// root, by workspace dir.
type Stamps map[string]stamp

// StateStamps stats the .agent-state of every ticket workspace under
// workRoot (kb workspaces aren't listed).
func StateStamps(workRoot string) Stamps {
	s := Stamps{}
	for _, dir := range List(workRoot, false) {
		var st stamp
		if fi, err := os.Stat(filepath.Join(dir, ".agent-state")); err == nil {
			st = stamp{exists: true, mod: fi.ModTime(), size: fi.Size(), fi: fi}
		}
		s[dir] = st
	}
	return s
}

// Changed tells whether b differs from a: a workspace came or went, or an
// .agent-state was created, deleted, modified or replaced.
func (a Stamps) Changed(b Stamps) bool {
	if len(a) != len(b) {
		return true
	}
	for dir, x := range a {
		y, ok := b[dir]
		if !ok || x.exists != y.exists {
			return true
		}
		if !x.exists {
			continue
		}
		if !x.mod.Equal(y.mod) || x.size != y.size || !os.SameFile(x.fi, y.fi) {
			return true
		}
	}
	return false
}
