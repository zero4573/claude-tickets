package assets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterialize(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_CACHE", t.TempDir())
	dir, err := Materialize("plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("the plugin manifest isn't there:", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatalf("hooks.json isn't valid JSON after the rewrite: %v", err)
	}
	exe, _ := executable()
	if !strings.Contains(string(data), `"command": "\"`+strings.ReplaceAll(exe, `\`, `\\`)+`\" hook agent-state`) {
		t.Errorf("hooks don't run this ct:\n%s", data)
	}
	if again, _ := Materialize("plugin"); again != dir {
		t.Errorf("a second call unpacked again: %s", again)
	}

	g, err := Materialize("graph")
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(g, "serve.sh")); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Error("serve.sh missing or not executable", err)
	}
	if _, err := os.Stat(filepath.Join(g, "Containerfile")); err != nil {
		t.Error(err)
	}
}

func TestHooksFor(t *testing.T) {
	got := HooksFor(`{"command": "ct hook agent-state idle"}`, `C:\Program Files\ct.exe`)
	var v map[string]string
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if v["command"] != `"C:\Program Files\ct.exe" hook agent-state idle` {
		t.Errorf("command = %q", v["command"])
	}
}
