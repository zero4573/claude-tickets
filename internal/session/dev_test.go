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
	if err := WriteSettings(dir, t.TempDir()); err != nil {
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

func TestDevGraphMCPNamesTheBuiltCt(t *testing.T) {
	dir := t.TempDir()
	ct := filepath.Join(DevDir(dir), "bin", "ct")
	file, err := WriteGraphMCPWith(dir, "vault", ct)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.MCPServers["graphify"]; got.Command != ct || len(got.Args) != 3 || got.Args[2] != dir {
		t.Errorf("graphify = %+v, want %s graph mcp %s", got, ct, dir)
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
