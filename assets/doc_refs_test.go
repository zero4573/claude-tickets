package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docRefRe matches `docs/<page>.md` followed, within a few characters, by a
// quoted heading: docs/x.md, "Heading" or docs/x.md ("Heading").
var docRefRe = regexp.MustCompile("(?:^|[^\"])docs/([A-Za-z0-9_-]+\\.md)`?[,:]?\\s*(?://\\s*)?\\(?\"([^\"]+)\"")

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			t.Fatal("no go.mod above " + dir)
		}
	}
}

func pageHeadings(t *testing.T, root, page string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", page))
	if err != nil {
		return nil
	}
	h := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "## ") {
			h[strings.TrimSpace(strings.TrimPrefix(l, "## "))] = true
		}
	}
	return h
}

// checkDocRefs returns one message per reference in text that names a page or
// heading that does not exist.
func checkDocRefs(t *testing.T, root, file, text string) []string {
	var bad []string
	for _, m := range docRefRe.FindAllStringSubmatch(text, -1) {
		h := pageHeadings(t, root, m[1])
		switch {
		case h == nil:
			bad = append(bad, file+": docs/"+m[1]+" does not exist")
		case !h[m[2]]:
			bad = append(bad, file+": docs/"+m[1]+" has no ## heading "+`"`+m[2]+`"`)
		}
	}
	return bad
}

func TestDocRefsNameExistingHeadings(t *testing.T) {
	root := repoRoot(t)
	var bad []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".cache", ".git", "node_modules", "docs":
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".go", ".md":
		default:
			return nil
		}
		if strings.HasSuffix(p, "doc_refs_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		bad = append(bad, checkDocRefs(t, root, rel, string(data))...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
}

func TestDocRefsCatchBadReference(t *testing.T) {
	root := repoRoot(t)
	if got := checkDocRefs(t, root, "x", `see docs/spec-workflow.md ("No Such Heading")`); len(got) != 1 {
		t.Errorf("bad heading not flagged: %v", got)
	}
	if got := checkDocRefs(t, root, "x", `see docs/nope.md, "Whatever"`); len(got) != 1 {
		t.Errorf("missing page not flagged: %v", got)
	}
	if got := checkDocRefs(t, root, "x", `see docs/spec-workflow.md ("Layout and format")`); len(got) != 0 {
		t.Errorf("good reference flagged: %v", got)
	}
}
