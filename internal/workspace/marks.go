package workspace

import (
	"encoding/json"
	"fmt"
)

// Mark records the point where a step (tidy) was done in a repo.
type Mark struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	At     string `json:"at"`
}

// Mark kinds.
const (
	MarkTidy = "tidy"
)

// MarkKinds lists the kinds in the order they are shown. An entry of another
// kind in workspace.json (an old docs mark) is ignored.
var MarkKinds = []string{MarkTidy}

// ValidMark reports whether kind is a mark kind.
func ValidMark(kind string) bool { return kind == MarkTidy }

// Mark states.
const (
	MarkOK    = "ok"
	MarkStale = "stale"
	MarkNone  = "none"
)

// MarkStatus compares a repo's mark of a kind with the current tree: ok
// when the recorded tree equals it (a rewrite of the history keeps it ok),
// stale when it differs, none when nothing is recorded.
func MarkStatus(r Repo, kind, tree string) string {
	m, ok := r.Marks[kind]
	switch {
	case !ok || m.Tree == "":
		return MarkNone
	case m.Tree == tree:
		return MarkOK
	}
	return MarkStale
}

// SetMark records m as the repo's mark of a kind, keeping the rest of the entry.
func SetMark(file, slug, kind string, m Mark) error {
	if !ValidMark(kind) {
		return fmt.Errorf("unknown mark kind %q", kind)
	}
	return Update(file, func(doc map[string]any) error {
		repos, _ := doc["repos"].([]any)
		for _, e := range repos {
			entry, ok := e.(map[string]any)
			if !ok || entry["slug"] != slug {
				continue
			}
			marks, _ := entry["marks"].(map[string]any)
			if marks == nil {
				marks = map[string]any{}
			}
			data, _ := json.Marshal(m)
			var v any
			_ = json.Unmarshal(data, &v)
			marks[kind] = v
			entry["marks"] = marks
			return nil
		}
		return fmt.Errorf("%s is not in the workspace", slug)
	})
}
