package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDevSettingsNeverSetPath(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	t.Setenv("CLAUDE_TICKETS_SESSION_SETTINGS", "")
	dir := t.TempDir()
	if err := WriteSettings(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Env["PATH"] != nil {
		t.Errorf("settings set PATH: %v", s.Env["PATH"])
	}
	if s.Env["GOPATH"] == nil {
		t.Error("the Go env is gone")
	}
}

func TestDevCommandPlugin(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CLAUDE", "")
	d := t.TempDir()
	argv := CommandPlugin("plugin-dir", d, filepath.Join(d, "missing"))
	want := []string{"claude", "--plugin-dir", "plugin-dir", "--add-dir=" + d}
	if len(argv) != len(want) {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %q, want %q", argv, want)
		}
	}
}
