package gitx

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOutInStdinAndStderr(t *testing.T) {
	d := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	blob, err := OutIn(d, strings.NewReader("hello\n"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := OutIn(d, strings.NewReader(blob+"\n"), "cat-file", "--batch-check")
	if err != nil || got != blob+" blob 6" {
		t.Fatalf("batch-check = %q, %v", got, err)
	}
	_, err = OutIn(d, nil, "rev-list", "nosuchrev")
	if err == nil || strings.HasPrefix(err.Error(), "exit status") {
		t.Fatalf("error should carry git's stderr: %v", err)
	}
}

func TestLinesStreamsAndStderr(t *testing.T) {
	d := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var got []string
	if err := Lines(d, func(l string) { got = append(got, l) }, "var", "-l"); err != nil || len(got) == 0 {
		t.Fatalf("lines = %v, %v", got, err)
	}
	for _, l := range got {
		if strings.HasSuffix(l, "\n") {
			t.Fatalf("line keeps its newline: %q", l)
		}
	}
	err := Lines(d, func(string) {}, "rev-list", "nosuchrev")
	if err == nil || strings.HasPrefix(err.Error(), "exit status") {
		t.Fatalf("error should carry git's stderr: %v", err)
	}
}
