package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseA = "# header\nfilters:\n  and:\n    - file.inFolder(\"projects\")\nviews:\n  - type: table\n    name: \"All\"\n"

func TestBaseKeyIgnoresFormatting(t *testing.T) {
	want := Key("x.base", []byte(baseA))
	for name, v := range map[string]string{
		"comments":   "filters:\n  and:\n    - file.inFolder(\"projects\")\nviews:\n  - type: table\n    name: All\n",
		"indent":     "filters:\n    and:\n          - file.inFolder(\"projects\")\nviews:\n    -   type: table\n        name: All\n",
		"key order":  "views:\n  - name: All\n    type: table\nfilters:\n  and:\n    - file.inFolder(\"projects\")\n",
		"quoting":    "filters: {and: ['file.inFolder(\"projects\")']}\nviews: [{type: 'table', name: \"All\"}]\n",
		"crlf":       strings.ReplaceAll(baseA, "\n", "\r\n"),
		"no trailer": strings.TrimSuffix(baseA, "\n"),
	} {
		if got := Key("x.base", []byte(v)); got != want {
			t.Errorf("%s: key differs", name)
		}
	}
}

func TestBaseKeyChangesOnContent(t *testing.T) {
	want := Key("x.base", []byte(baseA))
	for name, v := range map[string]string{
		"value":      strings.Replace(baseA, `"projects"`, `"tickets"`, 1),
		"list order": "filters:\n  and:\n    - file.inFolder(\"projects\")\nviews:\n  - name: All\n  - type: table\n",
		"type":       strings.Replace(baseA, "name: \"All\"", "name: 1", 1),
	} {
		if Key("x.base", []byte(v)) == want {
			t.Errorf("%s: key unchanged", name)
		}
	}
	a, b := "a: [1, 2]\n", "a: [2, 1]\n"
	if Key("x.base", []byte(a)) == Key("x.base", []byte(b)) {
		t.Error("list order ignored")
	}
}

func TestBaseKeyFallsBackToHash(t *testing.T) {
	for name, c := range map[string]struct{ rel, data string }{
		"not yaml":   {"x.base", "a: [unclosed\n"},
		"two docs":   {"x.base", "a: 1\n---\nb: 2\n"},
		"int keys":   {"x.base", "{1: a, [x]: b}\n"},
		"other file": {"x.md", "# c\na: 1\n"},
	} {
		if Key(c.rel, []byte(c.data)) != Hash([]byte(c.data)) {
			t.Errorf("%s: not Hash", name)
		}
	}
	if Key("x.md", []byte("a: 1")) == Key("x.md", []byte("a:  1")) {
		t.Error("non-base compared by YAML")
	}
}

func TestBaseKeyClassify(t *testing.T) {
	shipped := strings.TrimPrefix(baseA, "# header\n")
	src := source(t, map[string]string{"projects.base": shipped}, nil)
	reindented := "filters:\n    and:\n        - file.inFolder(\"projects\")\nviews:\n    - type: table\n      name: All\n"
	oldRaw := Hash([]byte(baseA)) // an old tix recorded the raw hash of the commented file
	src.History["projects.base"] = append(src.History["projects.base"], oldRaw, Key("projects.base", []byte(baseA)))
	rec := Record{Files: map[string]Shipped{"projects.base": {SHA256: oldRaw, Version: "1"}}}
	for name, c := range map[string]struct {
		data string
		want State
	}{
		"reindented":        {reindented, UpToDate},
		"as shipped":        {shipped, UpToDate},
		"old commented":     {baseA, UpToDate},
		"changed filter":    {strings.Replace(reindented, "projects", "tickets", 1), Edited},
		"old raw hash only": {"filters: {}\n", Edited},
	} {
		v := t.TempDir()
		if err := os.WriteFile(filepath.Join(v, "projects.base"), []byte(c.data), 0o644); err != nil {
			t.Fatal(err)
		}
		es, err := Classify(v, src, rec)
		if err != nil {
			t.Fatal(err)
		}
		if len(es) != 1 || es[0].State != c.want {
			t.Errorf("%s: %+v, want state %v", name, es, c.want)
		}
	}
}

func TestBaseKeyNoShippedBaseHasComments(t *testing.T) {
	src, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range src.Files {
		if !strings.HasSuffix(f.Rel, ".base") {
			continue
		}
		n++
		for i, l := range strings.Split(string(f.Data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				t.Errorf("%s:%d is a comment line: Obsidian drops it", f.Rel, i+1)
			}
		}
	}
	if n == 0 {
		t.Fatal("no shipped .base file")
	}
}
