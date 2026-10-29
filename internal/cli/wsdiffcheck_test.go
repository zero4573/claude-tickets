package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func diffCheckGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@acme.test", "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func diffCheckRun(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := wsDiffCheckCmd()
	var o, e bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return o.String(), e.String(), err
}

func TestDiffCheckCmd(t *testing.T) {
	d := t.TempDir()
	diffCheckGit(t, d, "init", "-q", "-b", "main")
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(d, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", []byte("a\n"))
	diffCheckGit(t, d, "add", "-A")
	diffCheckGit(t, d, "commit", "-q", "-m", "base")

	out, errs, err := diffCheckRun(t, "-C", d, "HEAD..HEAD")
	if err != nil || out != "" || errs != "" {
		t.Fatalf("clean: %q %q %v", out, errs, err)
	}

	write("x.bin", []byte{0, 1, 2})
	write("big.txt", []byte(strings.Repeat("y", 3000)))
	diffCheckGit(t, d, "add", "-A")
	diffCheckGit(t, d, "commit", "-q", "-m", "add")
	sha := diffCheckGit(t, d, "rev-parse", "--short", "HEAD")

	out, errs, err = diffCheckRun(t, "-C", d, "--max-size", "2K", "HEAD~1..HEAD")
	var ee exitError
	if !errors.As(err, &ee) || int(ee) != 3 {
		t.Fatalf("err = %v, want exit 3", err)
	}
	want := "large\t3000\t" + sha + "\tbig.txt\nbinary\t3\t" + sha + "\tx.bin\n"
	if out != want {
		t.Fatalf("stdout %q want %q", out, want)
	}
	if !strings.HasPrefix(errs, "ct ws diff-check: 2 file(s) flagged (binary or > 2K)\n") {
		t.Fatalf("stderr %q", errs)
	}

	for name, args := range map[string][]string{
		"bad range":     {"-C", d, "nope..HEAD"},
		"option range":  {"-C", d, "--", "--all"},
		"bad size":      {"-C", d, "--max-size", "zz", "HEAD"},
		"overflow size": {"-C", d, "--max-size", "9999999999G", "HEAD"},
		"non-repo":      {"-C", t.TempDir(), "HEAD"},
	} {
		_, _, err := diffCheckRun(t, args...)
		if err == nil || diffCheckExit(err) != 1 {
			t.Fatalf("%s: err = %v, exit %d, want exit 1", name, err, diffCheckExit(err))
		}
	}
	if _, _, err = diffCheckRun(t, "-C", d, "HEAD~1..HEAD"); diffCheckExit(err) != 3 {
		t.Fatalf("flagged: exit %d, want 3", diffCheckExit(err))
	}
	if _, _, err = diffCheckRun(t, "-C", d, "HEAD..HEAD"); diffCheckExit(err) != 0 {
		t.Fatalf("clean: exit %d, want 0", diffCheckExit(err))
	}
}

// diffCheckExit is the status Main gives err: exitError's own, else 1.
func diffCheckExit(err error) int {
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return int(ee)
	}
	return 1
}

func diffNoteRepo(t *testing.T) (string, func(name, body string), func(msg string) string) {
	t.Helper()
	d := t.TempDir()
	diffCheckGit(t, d, "init", "-q", "-b", "main")
	write := func(name, body string) {
		p := filepath.Join(d, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) string {
		diffCheckGit(t, d, "add", "-A")
		diffCheckGit(t, d, "commit", "-q", "-m", msg)
		return diffCheckGit(t, d, "rev-parse", "--short", "HEAD")
	}
	return d, write, commit
}

func TestDiffCheckNoteTestChanged(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tt.Log(1)\n}\n\nfunc TestTwo(t *testing.T) {}\n\nfunc helper() int { return 1 }\n")
	write("testdata/s/old.txtar", "x\n")
	write("testdata/s/gone.txtar", "x\n")
	commit("base")

	// edit a Test body: noted; a new Test and a new txtar: not noted
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tt.Log(2)\n}\n\nfunc TestTwo(t *testing.T) {}\n\nfunc TestNew(t *testing.T) {}\n\nfunc helper() int { return 1 }\n")
	write("testdata/s/new.txtar", "y\n")
	sha := commit("edit")
	out, errs, err := diffCheckRun(t, "-C", d, "HEAD~1..HEAD")
	if err != nil || errs != "" {
		t.Fatalf("err=%v stderr=%q", err, errs)
	}
	if want := "note\ttest-changed\tTestOne\t" + sha + "\ta_test.go\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}

	// only a non-test func edited: nothing
	write("a_test.go", strings.Replace(mustRead(t, d, "a_test.go"), "return 1", "return 2", 1))
	commit("helper")
	if out, _, err := diffCheckRun(t, "-C", d, "HEAD~1..HEAD"); err != nil || out != "" {
		t.Fatalf("helper edit: %q %v", out, err)
	}

	// a deleted Test and a modified and a deleted txtar
	write("a_test.go", strings.Replace(mustRead(t, d, "a_test.go"), "func TestTwo(t *testing.T) {}\n\n", "", 1))
	write("testdata/s/old.txtar", "changed\n")
	if err := os.Remove(filepath.Join(d, "testdata/s/gone.txtar")); err != nil {
		t.Fatal(err)
	}
	sha = commit("drop")
	out, _, err = diffCheckRun(t, "-C", d, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"note\ttest-changed\t-\t" + sha + "\ttestdata/s/gone.txtar\n",
		"note\ttest-changed\t-\t" + sha + "\ttestdata/s/old.txtar\n",
		"note\ttest-changed\tTestTwo\t" + sha + "\ta_test.go\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Count(out, "note") != 3 {
		t.Fatalf("unexpected notes: %q", out)
	}
}

func mustRead(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDiffCheckNoteUnsigned(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a.txt", "a\n")
	commit("base")
	write("a.txt", "b\n")
	sha := commit("next")

	out, errs, err := diffCheckRun(t, "-C", d, "HEAD~1..HEAD")
	if err != nil || out != "" || errs != "" {
		t.Fatalf("gpgsign false: %q %q %v", out, errs, err)
	}
	diffCheckGit(t, d, "config", "commit.gpgsign", "true")
	out, errs, err = diffCheckRun(t, "-C", d, "HEAD~1..HEAD")
	if err != nil || errs != "" {
		t.Fatalf("gpgsign true: %q %v", errs, err)
	}
	if want := "note\tunsigned\t-\t" + sha + "\t:commit\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func diffNoteOut(t *testing.T, d string, args ...string) string {
	t.Helper()
	out, errs, err := diffCheckRun(t, append([]string{"-C", d}, args...)...)
	if err != nil || errs != "" {
		t.Fatalf("err=%v stderr=%q", err, errs)
	}
	return out
}

func TestDiffCheckNoteGitlinkIsSkipped(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a.txt", "a\n")
	commit("base")
	h1 := strings.Repeat("1", 40)
	h2 := strings.Repeat("2", 40)
	diffCheckGit(t, d, "update-index", "--add", "--cacheinfo", "160000,"+h1+",x_test.go")
	diffCheckGit(t, d, "commit", "-q", "-m", "link")
	diffCheckGit(t, d, "update-index", "--cacheinfo", "160000,"+h2+",x_test.go")
	diffCheckGit(t, d, "commit", "-q", "-m", "bump")
	if out := diffNoteOut(t, d, "HEAD~1..HEAD"); out != "" {
		t.Fatalf("bump: %q", out)
	}
	diffCheckGit(t, d, "rm", "-q", "--cached", "x_test.go")
	diffCheckGit(t, d, "commit", "-q", "-m", "drop")
	if out := diffNoteOut(t, d, "HEAD~1..HEAD"); out != "" {
		t.Fatalf("removal: %q", out)
	}
}

func TestDiffCheckNoteBareGpgsign(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a.txt", "a\n")
	commit("base")
	write("a.txt", "b\n")
	sha := commit("next")
	f, err := os.OpenFile(filepath.Join(d, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[commit]\n\tgpgsign\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if want, out := "note\tunsigned\t-\t"+sha+"\t:commit\n", diffNoteOut(t, d, "HEAD~1..HEAD"); out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestDiffCheckNoteUnparsableAndNames(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n\nfunc Testdata() int { return 1 }\n")
	commit("base")
	// TestA edited into a syntax error, TestB deleted: one file note
	write("a_test.go", "package a\n\nfunc TestA( {\n")
	sha := commit("break")
	if want, out := "note\ttest-changed\t-\t"+sha+"\ta_test.go\n", diffNoteOut(t, d, "HEAD~1..HEAD"); out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	// a changed Testdata helper is no test
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n\nfunc Testdata() int { return 1 }\n")
	commit("fix")
	write("a_test.go", strings.Replace(mustRead(t, d, "a_test.go"), "return 1", "return 2", 1))
	commit("helper")
	if out := diffNoteOut(t, d, "HEAD~1..HEAD"); out != "" {
		t.Fatalf("Testdata edit: %q", out)
	}
	for name, want := range map[string]bool{"Test": true, "TestA": true, "Test_x": true, "Test1": true, "Testdata": false, "Testify": false, "Benchmarkx": false, "FuzzY": true, "Example": true, "Other": false} {
		if got := diffNoteIsTest(name); got != want {
			t.Errorf("diffNoteIsTest(%q) = %v", name, got)
		}
	}
}

func TestDiffCheckNoteLargeAndTypeChange(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	write("b_test.go", "package a\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")
	commit("base")
	// above --max-size: not parsed, one file note
	write("a_test.go", mustRead(t, d, "a_test.go")+"// "+strings.Repeat("x", 3000)+"\n")
	sha := commit("grow")
	out, _, _ := diffCheckRun(t, "-C", d, "--max-size", "2K", "HEAD~1..HEAD") // the large file itself exits 3
	if want := "note\ttest-changed\t-\t" + sha + "\ta_test.go\n"; !strings.HasSuffix(out, want) {
		t.Fatalf("large: %q want suffix %q", out, want)
	}
	// a test file replaced by a symlink
	if err := os.Remove(filepath.Join(d, "b_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a_test.go", filepath.Join(d, "b_test.go")); err != nil {
		t.Skip("no symlinks")
	}
	sha = commit("link")
	if want, out := "note\ttest-changed\t-\t"+sha+"\tb_test.go\n", diffNoteOut(t, d, "--max-size", "2K", "HEAD~1..HEAD"); out != want {
		t.Fatalf("type change: %q want %q", out, want)
	}
}

func TestDiffCheckNoteKeepsExit3AndSkipsMerges(t *testing.T) {
	d, write, commit := diffNoteRepo(t)
	write("a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	write("x/s.txtar", "x\n")
	write("x_test.go", "package a\n")
	commit("base")
	diffCheckGit(t, d, "checkout", "-q", "-b", "topic")
	write("a_test.go", strings.Replace(mustRead(t, d, "a_test.go"), "{}", "{ t.Log(1) }", 1))
	write("x/s.txtar", "y\n") // outside testdata/: not noted
	write("big.bin", "\x00\x01")
	sha := commit("edit")
	out, _, err := diffCheckRun(t, "-C", d, "HEAD~1..HEAD")
	if diffCheckExit(err) != 3 {
		t.Fatalf("exit %d, want 3", diffCheckExit(err))
	}
	if want := "binary\t2\t" + sha + "\tbig.bin\nnote\ttest-changed\tTestA\t" + sha + "\ta_test.go\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	// a merge commit is skipped: only the side commit is noted
	diffCheckGit(t, d, "checkout", "-q", "main")
	write("other.txt", "o\n")
	commit("main work")
	diffCheckGit(t, d, "merge", "-q", "--no-ff", "-m", "merge", "topic")
	out, _, _ = diffCheckRun(t, "-C", d, "HEAD~2..HEAD") // big.bin still exits 3
	if strings.Count(out, "note") != 1 || !strings.Contains(out, "\t"+sha+"\ta_test.go") {
		t.Fatalf("merge range: %q", out)
	}
}
