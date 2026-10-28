package diffcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func DiffCheckGit(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@acme.test", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func DiffCheckRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	DiffCheckGit(t, dir, "init", "-q", "-b", "main")
	return dir
}

func DiffCheckCommit(t testing.TB, dir, msg string, files map[string][]byte) string {
	t.Helper()
	for p, b := range files {
		full := filepath.Join(dir, p)
		if b == nil {
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	DiffCheckGit(t, dir, "add", "-A")
	DiffCheckGit(t, dir, "commit", "-q", "-m", msg)
	return DiffCheckGit(t, dir, "rev-parse", "--short", "HEAD")
}

func TestDiffCheckParseSize(t *testing.T) {
	for in, want := range map[string]int64{"2048": 2048, "1M": 1 << 20, "512K": 512 << 10, "2g": 2 << 30, "0": 0} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "x", "1.5M", "-1", "M", "9223372036854775807K", "8589934592G", "9223372036854775808"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) accepted", in)
		}
	}
}

func TestDiffCheckRangeOption(t *testing.T) {
	if _, err := Check(t.TempDir(), "--all", DefaultMaxSize); err == nil {
		t.Fatal("option-like range accepted")
	}
}

func TestDiffCheckClean(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.txt": []byte("a\n")})
	DiffCheckCommit(t, d, "more", map[string][]byte{"b.txt": []byte("b\n")})
	got, err := Check(d, "HEAD~1..HEAD", DefaultMaxSize)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDiffCheckBinaryAndLarge(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.txt": []byte("a\n")})
	sha := DiffCheckCommit(t, d, "add", map[string][]byte{
		"bin/x.dat": {0, 1, 2, 3},
		"big.txt":   []byte(strings.Repeat("line\n", 100)),
		"ok.txt":    []byte("ok\n"),
	})
	got, err := Check(d, "HEAD~1..HEAD", 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []Finding{
		{"large", 500, sha, "big.txt"},
		{"binary", 4, sha, "bin/x.dat"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v want %+v", got, want)
	}
	got, _ = Check(d, "HEAD~1..HEAD", 3)
	if len(got) != 2 || got[1].Kind != "binary,large" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiffCheckAddedThenDeletedStillFlagged(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.txt": []byte("a\n")})
	sha := DiffCheckCommit(t, d, "add", map[string][]byte{"out.txt": []byte(strings.Repeat("x", 300))})
	DiffCheckCommit(t, d, "remove", map[string][]byte{"out.txt": nil})
	got, err := Check(d, "HEAD~2..HEAD", 100)
	if err != nil || len(got) != 1 || got[0].Commit != sha || got[0].Kind != "large" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDiffCheckDeletionOnlyClean(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"big.bin": {0, 1, 2}, "a.txt": []byte("a\n")})
	DiffCheckCommit(t, d, "remove", map[string][]byte{"big.bin": nil})
	got, err := Check(d, "HEAD~1..HEAD", 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDiffCheckMergeCommitsSkipped(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.txt": []byte("a\n")})
	DiffCheckGit(t, d, "checkout", "-q", "-b", "side")
	DiffCheckCommit(t, d, "side", map[string][]byte{"s.txt": []byte("s\n")})
	DiffCheckGit(t, d, "checkout", "-q", "main")
	DiffCheckCommit(t, d, "main", map[string][]byte{"m.txt": []byte("m\n")})
	DiffCheckGit(t, d, "merge", "-q", "--no-ff", "-m", "merge", "side")
	got, err := Check(d, "HEAD~2..HEAD", 1)
	// the merge adds s.txt against its first parent but is skipped: only the
	// two real commits' files (2 bytes > 1) are flagged
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDiffCheckRootCommit(t *testing.T) {
	d := DiffCheckRepo(t)
	sha := DiffCheckCommit(t, d, "root", map[string][]byte{"x.bin": {0, 9}})
	got, err := Check(d, "HEAD", DefaultMaxSize)
	if err != nil || len(got) != 1 || got[0].Commit != sha || got[0].Kind != "binary" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDiffCheckEmptyRangeAndBadRange(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.bin": {0}})
	if got, err := Check(d, "HEAD..HEAD", 1); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	if _, err := Check(d, "nope..HEAD", 1); err == nil {
		t.Fatal("bad range accepted")
	}
	if _, err := Check(t.TempDir(), "HEAD", 1); err == nil {
		t.Fatal("non-repo accepted")
	}
}

func TestDiffCheckSpecialPath(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "base", map[string][]byte{"a.txt": []byte("a\n")})
	DiffCheckCommit(t, d, "add", map[string][]byte{"dir/ü file.bin": {0, 1}})
	got, err := Check(d, "HEAD~1..HEAD", DefaultMaxSize)
	if err != nil || len(got) != 1 || got[0].Path != "dir/ü file.bin" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
