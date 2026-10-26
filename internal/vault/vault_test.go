package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolVersionsOf(t *testing.T) {
	v := t.TempDir()
	write := func(c string) { os.WriteFile(filepath.Join(v, ".workflow.json"), []byte(c), 0o644) }
	got, _, w := ToolVersionsOf(v)
	if got["golang"] != "latest" || got["python"] != "latest" || len(got) != 2 || w != "" {
		t.Fatalf("default: %v %q", got, w)
	}
	write(`{"toolVersions":{"python":"3.13.7"}}`)
	if got, _, w := ToolVersionsOf(v); got["python"] != "3.13.7" || got["golang"] != "latest" || len(got) != 2 || w != "" {
		t.Fatalf("python only: %v %q", got, w)
	}
	write(`{"toolVersions":{"python":"none","golang":"","node":"22"}}`)
	if got, opt, _ := ToolVersionsOf(v); len(got) != 1 || got["node"] != "22" || strings.Join(opt, ",") != "golang,python" {
		t.Fatalf("dropped: %v", got)
	}
	for _, bad := range []string{`{not json`, `{"toolVersions":{"python":3}}`} {
		write(bad)
		got, _, w := ToolVersionsOf(v)
		if got["golang"] != "latest" || got["python"] != "latest" || len(got) != 2 || !strings.Contains(w, ".workflow.json") {
			t.Fatalf("%s: %v %q", bad, got, w)
		}
	}
}
