package watch

import (
	"bytes"
	"testing"
)

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestDrawFrame(t *testing.T) {
	var w countingWriter
	if err := DrawFrame(&w, []string{"short", "exactly10!", "x"}, 10); err != nil {
		t.Fatal(err)
	}
	if w.writes != 1 {
		t.Errorf("%d writes, want 1", w.writes)
	}
	// erase after short lines only, no newline after the last one
	want := "\x1b[H" + "short\x1b[K" + "\r\n" + "exactly10!" + "\r\n" + "x\x1b[K" + "\x1b[J"
	if got := w.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDrawFrameEmpty(t *testing.T) {
	var w bytes.Buffer
	if err := DrawFrame(&w, nil, 80); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[H\x1b[J"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEnterLeave(t *testing.T) {
	var w bytes.Buffer
	if err := Enter(&w); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[?1049h\x1b[H\x1b[2J\x1b[?25l"; got != want {
		t.Errorf("Enter wrote %q, want %q", got, want)
	}
	w.Reset()
	if err := Leave(&w); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "\x1b[?25h\x1b[?1049l"; got != want {
		t.Errorf("Leave wrote %q, want %q", got, want)
	}
}
