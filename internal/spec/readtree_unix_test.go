//go:build !windows

package spec

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadRegularRefusesFIFOAndSymlink(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	os.WriteFile(good, []byte("x"), 0o644)
	fifo := filepath.Join(dir, "fifo.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("no mkfifo:", err)
	}
	link := filepath.Join(dir, "link.md")
	os.Symlink(good, link)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, p := range []string{fifo, link} {
			if _, ok, err := readRegular(p); ok || err != nil {
				t.Errorf("readRegular(%s) = ok %v, err %v; want skipped", filepath.Base(p), ok, err)
			}
		}
		if b, ok, err := readRegular(good); !ok || err != nil || string(b) != "x" {
			t.Errorf("readRegular(good) = %q, %v, %v", b, ok, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("readRegular blocked on a FIFO")
	}
}

func TestReadTreeSkipsOversize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "big.md"), []byte(strings.Repeat("a", MaxSpecSize+1)), 0o644)
	os.WriteFile(filepath.Join(dir, "ok.md"), []byte("ok"), 0o644)
	tr, err := ReadTree(dir)
	if err != nil || len(tr) != 1 || string(tr["ok.md"]) != "ok" {
		t.Fatalf("tree = %v, %v", tr, err)
	}
}
