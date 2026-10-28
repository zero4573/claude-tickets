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
