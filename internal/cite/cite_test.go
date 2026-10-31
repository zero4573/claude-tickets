package cite

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sampleNote = "---\n" +
	"title: flow\n" +
	"note: `fake.go:1` in frontmatter is no citation\n" +
	"---\n" +
	"# Flow\n" +
	"Pinned commits: billing-service `main` @ `aaaaaaa` and [[github-acme-orders_api|orders_api]] @ `bbbbbbb`.\n" +
	"\n" +
	"**billing-service** · `pkg/retry/retry.go` · `Retry`, `backoff` · lines 3-8\n" +
	"\n" +
	"```go\n" +
	"func Retry(n int) error {\n" +
	"\t// …\n" +
	"\treturn backoff(n)\n" +
	"}\n" +
	"```\n" +
	"\n" +
	"The handler (`orders_api:lib/api.ex:2-3`) and `retry.go:4`; not `localhost:8080` nor `127.0.0.1:5432`.\n" +
	"```\n" +
	"`inside.go:1` in a block is no citation\n" +
	"```\n" +
	"- Listed:\n" +
	"  **billing-service** · `pkg/retry/retry.go` · lines 1-1\n" +
	"  ```go\n" +
	"  package retry\n" +
	"  ```\n"

func TestParse(t *testing.T) {
	n := Parse(sampleNote)
	if want := map[string]string{"billing-service": "aaaaaaa", "orders-api": "bbbbbbb"}; !reflect.DeepEqual(n.Pins, want) {
		t.Errorf("Pins = %v, want %v", n.Pins, want)
	}
	type c struct {
		line       int
		repo, path string
		start, end int
		caption    bool
		symbols    []string
		chunk      int // lines, -1 for none
	}
	var got []c
	for _, x := range n.Citations {
		k := -1
		if x.Chunk != nil {
			k = len(x.Chunk)
		}
		got = append(got, c{x.Line, x.Repo, x.Path, x.Start, x.End, x.Caption, x.Symbols, k})
	}
	want := []c{
		{8, "billing-service", "pkg/retry/retry.go", 3, 8, true, []string{"Retry", "backoff"}, 4},
		{17, "orders_api", "lib/api.ex", 2, 3, false, nil, -1},
		{17, "", "retry.go", 4, 4, false, nil, -1},
		{22, "billing-service", "pkg/retry/retry.go", 1, 1, true, nil, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Citations:\n got %+v\nwant %+v", got, want)
	}
	if ch := n.Citations[3].Chunk; ch[0] != "package retry" {
		t.Errorf("an indented block's lines are dedented: %q", ch)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// a repo with pkg/retry/retry.go and two api.ex, committed; returns its
// dir and the commit
func repoAt(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	files := map[string]string{
		"pkg/retry/retry.go": "package retry\n\nfunc Retry(n int) error {\n\tif n < 0 {\n\t\treturn nil\n\t}\n\treturn backoff(n)\n}\n",
		"lib/api.ex":         "defmodule Api do\n  def create(x), do: x\nend\n",
		"test/api.ex":        "x\n",
		"blank_end.txt":      "a\n\n\n",
		"crlf.txt":           "a\r\nb\r\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir, git(t, dir, "rev-parse", "HEAD")
}

func checker(dirs map[string]string, symbols bool) *Checker {
	return &Checker{Symbols: symbols, Resolve: func(key string) (string, error) {
		if d, ok := dirs[key]; ok {
			return d, nil
		}
		return "", errors.New("no clone for " + key)
	}}
}

func reasons(fs []Failure) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Reason)
	}
	return out
}

func TestCheckPasses(t *testing.T) {
	dir, sha := repoAt(t)
	note := strings.NewReplacer("aaaaaaa", sha, "bbbbbbb", sha).Replace(sampleNote)
	c := checker(map[string]string{"billing-service": dir, "orders-api": dir}, true)
	// retry.go:4 is in both pinned repos (one clone here): ambiguous; the
	// rest holds
	got := reasons(c.Check(Parse(note)))
	if len(got) != 1 || !strings.HasPrefix(got[0], "ambiguous: billing-service:pkg/retry/retry.go, orders-api:pkg/retry/retry.go") {
		t.Errorf("failures = %q", got)
	}
	c = checker(map[string]string{"billing-service": dir}, true)
	one := "billing-service @ `" + sha + "`\n\n`retry.go:4` and `api.ex:2` and `lib/api.ex:1-3`\n"
	if got := reasons(c.Check(Parse(one))); len(got) != 1 || got[0] != "ambiguous: lib/api.ex, test/api.ex" {
		t.Errorf("failures = %q", got)
	}
}

func TestCheckFailures(t *testing.T) {
	dir, sha := repoAt(t)
	head := "billing-service `main` @ `" + sha + "`\n\n"
	for name, tc := range map[string]struct {
		body    string
		symbols bool
		want    string
	}{
		"missing file":     {"`pkg/nope.go:3`", false, "pkg/nope.go is not in billing-service at "},
		"past the end":     {"`pkg/retry/retry.go:7-12`", false, "lines 7-12 are outside pkg/retry/retry.go at " + sha[:12] + " (8 lines)"},
		"bad range":        {"`pkg/retry/retry.go:5-3`", false, "bad range 5-3"},
		"unpinned repo":    {"`orders:lib/api.ex:1`", false, "no pinned commit for orders"},
		"not a commit":     {"**billing-service** · `pkg/retry/retry.go` · lines 1-1\n", false, ""},
		"chunk differs":    {"**billing-service** · `pkg/retry/retry.go` · lines 3-4\n```go\nfunc Retry(n uint) error {\n\tif n < 0 {\n```\n", false, "chunk line 1 differs from line 3: has \"func Retry(n uint) error {\", the file \"func Retry(n int) error {\""},
		"chunk short":      {"**billing-service** · `pkg/retry/retry.go` · lines 3-5\n```go\nfunc Retry(n int) error {\n```\n", false, "chunk ends at line 3, the caption says 5"},
		"chunk long":       {"**billing-service** · `pkg/retry/retry.go` · lines 7-7\n```go\n\treturn backoff(n)\n}\n```\n", false, "chunk line 2 is past line 7, the caption's end"},
		"elided not found": {"**billing-service** · `pkg/retry/retry.go` · lines 3-8\n```go\nfunc Retry(n int) error {\n\t// ...\n\treturn other(n)\n}\n```\n", false, "chunk line 3 (after an elision) is not in lines 4-8"},
		"symbol missing":   {"**billing-service** · `pkg/retry/retry.go` · `retry.Jitter()` · lines 3-8\n", true, "symbol Jitter is not in lines 3-8 of pkg/retry/retry.go"},
		"symbol unchecked": {"**billing-service** · `pkg/retry/retry.go` · `retry.Jitter()` · lines 3-8\n", false, ""},
		"no clone":         {"**orders** · `lib/api.ex` · lines 1-1\n", false, "no clone for orders"},
		"no pin at all":    {"`lib/api.ex:1`", false, "no pinned commit in the note"},
		"blank last lines": {"`blank_end.txt:3` and `blank_end.txt:4`", false, "lines 4-4 are outside blank_end.txt at " + sha[:12] + " (3 lines)"},
		"crlf chunk":       {"**billing-service** · `crlf.txt` · lines 1-2\n```\na\nb\n```\n", false, ""},
		"dedented chunk":   {"**billing-service** · `pkg/retry/retry.go` · lines 4-6\n```go\nif n < 0 {\n\treturn nil\n}\n```\n", false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			src := head + tc.body
			pins := map[string]string{}
			switch name {
			case "no pin at all":
				src = tc.body
			case "not a commit":
				src = "billing-service @ `" + strings.Repeat("0", 40) + "`\n\n" + tc.body
				tc.want = strings.Repeat("0", 40) + " is not a commit in " + dir
			case "no clone":
				pins["orders"] = sha
			}
			c := checker(map[string]string{"billing-service": dir}, tc.symbols)
			c.Pins = pins
			got := reasons(c.Check(Parse(src)))
			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("failures = %q, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("failures = %q, want one starting %q", got, tc.want)
			}
		})
	}
}

func TestElision(t *testing.T) {
	for l, want := range map[string]bool{
		"\t// …":            true,
		"# ... (12 lines)":  true,
		"  -- …":            true,
		"/* … */":           true,
		"…":                 true,
		"...":               true,
		"...rest,":          false,
		"// a real comment": false,
		"return x // …":     false,
	} {
		if got := elisionRe.MatchString(l); got != want {
			t.Errorf("elision(%q) = %v, want %v", l, got, want)
		}
	}
}

func TestSymbolName(t *testing.T) {
	for in, want := range map[string]string{
		"Retry": "Retry", "retry.Jitter()": "Jitter", "Api::create": "create",
		"Order#total": "total", "upsert_by(attrs)": "upsert_by", "()": "",
	} {
		if got := symbolName(in); got != want {
			t.Errorf("symbolName(%q) = %q, want %q", in, got, want)
		}
	}
}
