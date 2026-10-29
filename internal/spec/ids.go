package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// IDRef is a requirement ID found in a file.
type IDRef struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Line int    `json:"line"`
}

var idHeadRe = regexp.MustCompile(`^###[ \t]+(REQ-[A-Z][A-Z0-9]{1,7}-[1-9][0-9]*)\b`)

// FindIDs lists the requirement IDs defined by "### REQ-..." headings in
// src (outside code fences), in order, without parsing the blocks, so
// it also works on files that don't parse (the duplicate-ID check).
func FindIDs(name string, src []byte) []IDRef {
	var out []IDRef
	fence := false
	for i, ln := range splitLines(src) {
		if isFence(ln) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if m := idHeadRe.FindStringSubmatch(ln); m != nil {
			out = append(out, IDRef{ID: m[1], File: name, Line: i + 1})
		}
	}
	return out
}

// Duplicates returns, per ID defined more than once in refs, all its
// definitions (in input order), sorted by ID.
func Duplicates(refs []IDRef) map[string][]IDRef {
	by := map[string][]IDRef{}
	for _, r := range refs {
		by[r.ID] = append(by[r.ID], r)
	}
	for id, l := range by {
		if len(l) < 2 {
			delete(by, id)
		}
	}
	return by
}

// SortedIDs returns the keys of a Duplicates result, sorted.
func SortedIDs(m map[string][]IDRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Capability is a capability folder: <root>/<name>/spec.md.
type Capability struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	File string `json:"file"`
}

// Capabilities lists the capability folders of a specs root (a repo's
// specs/ or the vault's projects/<slug>/specs/): the subfolders holding a
// spec.md, sorted by name. A missing root gives none.
func Capabilities(root string) ([]Capability, error) {
	if st, err := os.Lstat(root); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Capability
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		f := filepath.Join(root, e.Name(), "spec.md")
		if st, err := os.Lstat(f); err == nil && st.Mode().IsRegular() {
			out = append(out, Capability{Name: e.Name(), Dir: filepath.Join(root, e.Name()), File: f})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ParseAll parses every capability under root.
func ParseAll(root string) ([]*Spec, error) {
	caps, err := Capabilities(root)
	if err != nil {
		return nil, err
	}
	var out []*Spec
	for _, c := range caps {
		s, err := ParseFile(c.File)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// RootIDs lists the requirement IDs defined under a specs root, by
// FindIDs over each capability's spec.md.
func RootIDs(root string) ([]IDRef, error) {
	caps, err := Capabilities(root)
	if err != nil {
		return nil, err
	}
	var out []IDRef
	for _, c := range caps {
		b, err := readSpec(c.File)
		if err != nil {
			return nil, err
		}
		out = append(out, FindIDs(c.File, b)...)
	}
	return out, nil
}
