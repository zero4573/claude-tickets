package gitx

import (
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// list renders entries as git config --list -z does: key\nvalue\0, or
// key\0 for a bare key.
func list(ents []confEntry) string {
	var b strings.Builder
	for _, e := range ents {
		b.WriteString(e.key())
		if e.hasValue {
			b.WriteString("\n" + e.value)
		}
		b.WriteByte(0)
	}
	return b.String()
}

func TestParseConfig(t *testing.T) {
	for _, c := range []struct {
		name, in, want string // want "" with err: a refusal
		err            bool
	}{
		{"plain", "[core]\n\tbare = false\n", "core.bare\nfalse\x00", false},
		{"bare key", "[core]\nbare\n", "core.bare\x00", false},
		{"empty value", "[core]\nbare =\n", "core.bare\n\x00", false},
		{"case", "[CoRe]\nBaRe = X\n", "core.bare\nX\x00", false},
		{"subsection keeps case", "[remote \"Up\"]\nURL = u\n", "remote.Up.url\nu\x00", false},
		{"old form lowercased", "[filter.LFS]\nsmudge = s\n", "filter.lfs.smudge\ns\x00", false},
		{"old form nested", "[a.B.c]\nk = v\n", "a.b.c.k\nv\x00", false},
		{"subsection escapes", "[s \"a\\\"b\\\\c\\d\"]\nk = v\n", "s.a\"b\\cd.k\nv\x00", false},
		{"key on the header line", "[core] bare=false\n", "core.bare\nfalse\x00", false},
		{"two keys", "[a]\nx=1\ny=2\n", "a.x\n1\x00a.y\n2\x00", false},
		{"quotes concatenate", "[a]\nk = \"a\" b\n", "a.k\na b\x00", false},
		{"quoted spaces kept", "[a]\nk = \"  a  \"\n", "a.k\n  a  \x00", false},
		{"inner spaces kept", "[a]\nk = a   b\t c  \n", "a.k\na   b\t c\x00", false},
		{"escapes", "[a]\nk = x\\ty\\nz\\b\\\\\\\"\n", "a.k\nx\ty\nz\b\\\"\x00", false},
		{"bad escape", "[a]\nk = \\q\n", "", true},
		{"comment after value", "[a]\nk = v # c\nl = w ; c\n", "a.k\nv\x00a.l\nw\x00", false},
		{"hash in quotes", "[a]\nk = \"v # c\"\n", "a.k\nv # c\x00", false},
		{"continuation", "[a]\nk = one \\\n two\n", "a.k\none  two\x00", false},
		{"comment backslash does not continue", "[core]\n# note \\\nsshCommand = evil\n", "core.sshcommand\nevil\x00", false},
		{"semicolon comment backslash", "[core]\n; note \\\nfsmonitor = evil\n", "core.fsmonitor\nevil\x00", false},
		{"crlf", "[a]\r\nk = v\r\n", "a.k\nv\x00", false},
		{"bom", "\xef\xbb\xbf[a]\nk = v\n", "a.k\nv\x00", false},
		{"no final newline", "[a]\nk = v", "a.k\nv\x00", false},
		{"backslash at eof", "[a]\nk = v\\", "a.k\nv\x00", false},
		{"unterminated quote", "[a]\nk = \"v\n", "", true},
		{"unterminated header", "[a\nk = v\n", "", true},
		{"key outside a section", "k = v\n", "", true},
		{"key with a comment, no =", "[a]\nk # c\n", "", true},
		{"key with digits and dash", "[a]\nk-2 = v\n", "a.k-2\nv\x00", false},
		{"key starting with a digit", "[a]\n2k = v\n", "", true},
		{"bad section char", "[a_b]\nk = v\n", "", true},
		{"control char in subsection", "[a \"x\x01\"]\nk = v\n", "", true},
		{"newline in subsection", "[a \"x\ny\"]\nk = v\n", "", true},
		{"no space before quote", "[a\"x\"]\nk = v\n", "", true},
		{"nul byte", "[a]\nk = v\x00\n", "", true},
		{"empty section", "[]\nk = v\n", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseConfig([]byte(c.in), "f")
			if c.err {
				if err == nil {
					t.Fatalf("parsed %q as %q, want an error", c.in, list(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("parse %q: %v", c.in, err)
			}
			if list(got) != c.want {
				t.Fatalf("parse %q = %q, want %q", c.in, list(got), c.want)
			}
		})
	}
}

func TestParseConfigLines(t *testing.T) {
	ents, err := parseConfig([]byte("# c\n[a]\n\nk = 1 \\\n 2\nl = 3\n[b] m = 4\n"), "f")
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, e := range ents {
		lines = append(lines, e.line)
	}
	if len(lines) != 3 || lines[0] != 4 || lines[1] != 6 || lines[2] != 7 {
		t.Fatalf("lines %v, want [4 6 7]", lines)
	}
}

// gitList is git's own reading of a config file (ok false: git refuses it).
func gitList(t *testing.T, dir string, data []byte) (string, bool) {
	t.Helper()
	f := filepath.Join(dir, "config")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "config", "-f", f, "--list", "-z", "--no-includes")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.Output()
	return string(out), err == nil
}

// differential checks that the Go parse equals git's, or refuses.
func differential(t *testing.T, dir string, data []byte) {
	t.Helper()
	ents, err := parseConfig(data, "f")
	want, ok := gitList(t, dir, data)
	switch {
	case err != nil:
		// refusing is always allowed
	case !ok:
		t.Errorf("git refuses %q, the Go parser read %q", data, list(ents))
	case list(ents) != want:
		t.Errorf("config %q: Go reads %q, git reads %q", data, list(ents), want)
	}
}

// configSeeds are configs both the differential test and the fuzz
// target start from.
var configSeeds = []string{
	"[core]\n\tbare = false\n\trepositoryformatversion = 0\n",
	"[remote \"origin\"]\n\turl = git@example.com:acme/x.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
	"[core]\n# note \\\nsshCommand = evil\n",
	"[a]\nk = \"a\" b \"c\" ; x\n",
	"[a]\nk = one \\\n two \\\n\n",
	"[filter.LFS]\nsmudge = x\n[Filter \"LFS\"]\nclean = y\n",
	"\xef\xbb\xbf[a]\r\nk = v\r\nl\r\n",
	"[core] bare=false\n[x \"\\\\\\\"\"] y\n",
	"[a]\nk = \"\\t\\n\\b\\\\\\\"\"\n",
	"[a]k=v\n[b]\n  ; c\n  # d\n m =\n",
}

func TestParseConfigMatchesGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	for _, s := range configSeeds {
		differential(t, dir, []byte(s))
	}
	// Random configs from git's syntax characters
	alphabet := []string{"[", "]", "\"", "\\", "\n", "\r\n", "#", ";", "=", ".", " ", "\t",
		"a", "B", "k", "-", "1", "x", "core", "remote", "[a]", "[b \"S\"]", "[c.D]", "\nk = ", "\nv",
		"[a.]", "[a..b]", "[.a]", "[a \"x\" ]", "[a  \"y\"]", "\x01", "\r", "\v", "\f", "\xc3\xa9",
		// escapes mixed with quotes, spaces and continuations; includes;
		// long and high-byte names
		"t", "n", "b", "\\t", "\\n", "\"\\ ", "\r\\", "\\\n", "[include]", "[includeIf \"gitdir:x\"]", "path",
		"\npath = ", "\xff", "k\xff", strings.Repeat("k", 300)}
	r := rand.New(rand.NewSource(1))
	n := 5000
	if v, err := strconv.Atoi(os.Getenv("GITX_DIFF_N")); err == nil {
		n = v // a longer run: GITX_DIFF_N=50000 go test -run MatchesGit
	}
	if testing.Short() {
		n = 50
	}
	for i := 0; i < n; i++ {
		var b bytes.Buffer
		b.WriteString([]string{"[a]\n", "[s \"x\"]\n", "[p.Q]\n", ""}[r.Intn(4)])
		for j := r.Intn(25); j >= 0; j-- {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
		}
		differential(t, dir, b.Bytes())
	}
}

// FuzzParseConfig is the differential test as a fuzz target: git is the
// oracle (go test -fuzz FuzzParseConfig ./internal/gitx).
func FuzzParseConfig(f *testing.F) {
	if _, err := exec.LookPath("git"); err != nil {
		f.Skip("no git")
	}
	for _, s := range configSeeds {
		f.Add([]byte(s))
	}
	f.Add([]byte("[a]\nk = \"\\ x\\t\" \\\r\n y\n[include]\npath = z\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		differential(t, t.TempDir(), data)
	})
}
