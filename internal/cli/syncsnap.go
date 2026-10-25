package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// stamp is what a snapshot records per file: enough to tell an edit.
// mtime+size is a deliberate limit: a same-size edit within one mtime tick
// (coarse-timestamp filesystems) goes unseen here until the next search's
// stale check catches it. Accepted (plan.md Decisions); no hashing.
type stamp struct {
	mtime int64
	size  int64
}

// snapshotTickets lists the regular files under <vault>/tickets (vault-relative,
// "/"-separated). A missing tickets/ is an empty snapshot.
func snapshotTickets(vault string) (map[string]stamp, error) {
	snap := map[string]stamp{}
	root := filepath.Join(vault, "tickets")
	st, err := os.Stat(root)
	if os.IsNotExist(err) {
		return snap, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		snap[relTo(vault, p)] = stamp{info.ModTime().UnixNano(), info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// changedPaths is the paths changed, added or removed between two snapshots, sorted.
func changedPaths(before, after map[string]stamp) []string {
	var out []string
	for p, s := range after {
		if b, ok := before[p]; !ok || b != s {
			out = append(out, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// refreshAfterSync refreshes the index for what a sync run changed: the
// snapshot diff plus the follow-up note (vault-relative, "" if none). Nothing
// changed: no refresh. Either snapshot failed: the whole vault.
func refreshAfterSync(vault string, before map[string]stamp, beforeErr error, noteRel string) {
	after, err := snapshotTickets(vault)
	if beforeErr != nil || err != nil {
		refreshIndex(vault)
		return
	}
	paths := changedPaths(before, after)
	if noteRel != "" {
		paths = append(paths, noteRel)
	}
	if len(paths) > 0 {
		refreshIndex(vault, paths...)
	}
}
