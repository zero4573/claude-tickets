package followup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSameMinute(t *testing.T) {
	v := t.TempDir()
	os.Mkdir(filepath.Join(v, ".obsidian"), 0o755)
	a, err := Write(v, "ticket-sync", "ticket-sync", []string{"first"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(v, "ticket-sync", "ticket-sync", []string{"second"})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("both runs wrote %s", a)
	}
	data, _ := os.ReadFile(a)
	if !strings.Contains(string(data), "- [ ] first") {
		t.Errorf("the first note was overwritten:\n%s", data)
	}
	data, _ = os.ReadFile(b)
	name := strings.TrimSuffix(filepath.Base(b), ".md")
	if !strings.HasSuffix(name, "-2") || !strings.Contains(string(data), "title: "+name+"\n") {
		t.Errorf("second note %s:\n%s", b, data)
	}
	if none, _ := Write(v, "x", "x", []string{" ", ""}); none != "" {
		t.Error("no tasks should write no note")
	}
}
