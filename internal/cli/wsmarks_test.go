package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmSign(t *testing.T) {
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false, "yep\n": false} {
		dir := t.TempDir()
		inF := filepath.Join(dir, "in")
		if err := os.WriteFile(inF, []byte(in), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(inF)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dir, "out"))
		if err != nil {
			t.Fatal(err)
		}
		if got := confirmSign(f, out); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
		f.Close()
		out.Close()
	}
}

func TestSignAskNeedsOnlyStdinTerminal(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) *os.File {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := signAsk(func() bool { return false }, write("a", "y\n"), out); err == nil {
		t.Error("no terminal on stdin: want a refusal")
	}
	if err := signAsk(func() bool { return true }, write("b", "y\n"), out); err != nil {
		t.Errorf("terminal stdin, yes: %v", err)
	}
	if err := signAsk(func() bool { return true }, write("c", "n\n"), out); err == nil {
		t.Error("terminal stdin, no: want a refusal")
	}
}
