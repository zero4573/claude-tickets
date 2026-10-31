package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkInstalls(t *testing.T, tool string, versions ...string) {
	t.Helper()
	for _, v := range versions {
		if err := os.MkdirAll(filepath.Join(os.Getenv("ASDF_DATA_DIR"), "installs", tool, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mkRepo(t *testing.T, root, slug, gomod string) string {
	t.Helper()
	d := filepath.Join(root, slug)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if gomod != "" {
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte(gomod), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestWriteToolVersions(t *testing.T) {
	type repo struct{ slug, gomod string }
	tests := []struct {
		name     string
		setting  string // .workflow.json, "" for none
		golang   []string
		python   []string
		repos    []repo
		want     string // exact lines after the two header lines, "" = no file
		warnings []string
	}{
		{"go line only", "", []string{"1.25.5", "1.26.1"}, []string{"3.12.3"},
			[]repo{{"a", "module a\n\ngo 1.25\n"}}, "golang 1.25.5\npython 3.12.3\n", nil},
		{"toolchain line wins", "", []string{"1.25.5", "1.26.1"}, []string{"3.12.3"},
			[]repo{{"a", "go 1.25\ntoolchain go1.26.1\n"}}, "golang 1.26.1\npython 3.12.3\n", nil},
		{"two repos highest", "", []string{"1.21.7", "1.25.5", "1.26.1"}, []string{"3.12.3"},
			[]repo{{"a", "go 1.21\n"}, {"b", "go 1.25.2\n"}}, "golang 1.25.5\npython 3.12.3\n", nil},
		{"same X.Y preferred over newer", "", []string{"1.25.1", "1.25.5", "1.27.3"}, []string{"3.12.3"},
			[]repo{{"a", "go 1.25\n"}}, "golang 1.25.5\npython 3.12.3\n", nil},
		{"newer X.Y when no same X.Y", "", []string{"1.21.7", "1.27.3"}, []string{"3.12.3"},
			[]repo{{"a", "go 1.25\n"}}, "golang 1.27.3\npython 3.12.3\n", nil},
		{"go.mod too new", "", []string{"1.21.7"}, []string{"3.12.3"},
			[]repo{{"a", "go 1.26.1\n"}}, "python 3.12.3\n",
			[]string{"tix: .tool-versions: golang 1.26.1 isn't installed (asdf install golang 1.26.1)"}},
		{"nothing installed", "", nil, nil, nil, "",
			[]string{"tix: .tool-versions: golang latest isn't installed (asdf install golang latest)",
				"tix: .tool-versions: python latest isn't installed (asdf install python latest)"}},
		{"latest without go.mod", "", []string{"1.9.0", "1.26.1", "1.100.0"}, nil,
			[]repo{{"a", ""}}, "golang 1.100.0\n", []string{"tix: .tool-versions: python latest isn't installed (asdf install python latest)"}},
		{"setting overrides", `{"toolVersions":{"python":"3.12.3","golang":"1.21.7"}}`, []string{"1.21.7", "1.26.1"}, []string{"3.12.3", "3.13.7"},
			nil, "golang 1.21.7\npython 3.12.3\n", nil},
		{"user version not installed", `{"toolVersions":{"python":"3.99.0"}}`, nil, []string{"3.12.3"},
			nil, "", []string{"tix: .tool-versions: golang latest isn't installed (asdf install golang latest)",
				"tix: .tool-versions: python 3.99.0 isn't installed (asdf install python 3.99.0)"}},
		{"go.mod beats setting golang", `{"toolVersions":{"golang":"1.21.7"}}`, []string{"1.21.7", "1.26.1"}, nil,
			[]repo{{"a", "go 1.26\n"}}, "golang 1.26.1\n", []string{"tix: .tool-versions: python latest isn't installed (asdf install python latest)"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "asdf"))
			os.MkdirAll(filepath.Join(root, "asdf", "installs"), 0o755) // asdf present, maybe nothing installed
			mkInstalls(t, "golang", tc.golang...)
			mkInstalls(t, "python", tc.python...)
			vaultDir := filepath.Join(root, "vault")
			ws := filepath.Join(root, "ws")
			for _, d := range []string{vaultDir, ws} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.setting != "" {
				if err := os.WriteFile(filepath.Join(vaultDir, ".workflow.json"), []byte(tc.setting), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			dirs := map[string]string{}
			for _, r := range tc.repos {
				dirs[r.slug] = mkRepo(t, ws, r.slug, r.gomod)
			}
			var warns []string
			if err := WriteToolVersions(ws, vaultDir, dirs, func(m string) { warns = append(warns, m) }); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(ws, ".tool-versions"))
			if tc.want == "" {
				if err == nil {
					t.Fatalf("file written: %s", data)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.SplitAfterN(string(data), "\n", 3)
				if len(lines) != 3 || lines[0] != toolVersionsHeader+"\n" || !strings.HasPrefix(lines[1], "# golang: ") || lines[2] != tc.want {
					t.Fatalf("got %q, want body %q", data, tc.want)
				}
			}
			if strings.Join(warns, "|") != strings.Join(tc.warnings, "|") {
				t.Fatalf("warnings %q, want %q", warns, tc.warnings)
			}
		})
	}
}

func TestWriteToolVersionsStale(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "asdf"))
	f := filepath.Join(root, ".tool-versions")
	os.WriteFile(f, []byte(toolVersionsHeader+"\ngolang 1.0.0\n"), 0o644)
	if err := WriteToolVersions(root, root, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("stale file kept")
	}
	// a file the user wrote is left alone when nothing resolves
	os.WriteFile(f, []byte("golang 1.0.0\n"), 0o644)
	if err := WriteToolVersions(root, root, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("user file removed")
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.26", "1.26.0", 0}, {"1.9.0", "1.10.0", -1}, {"1.26.1", "1.26", 1}, {"abc", "1.0", -1}} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s: %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestWriteToolVersionsNoAsdf(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "none"))
	var warned []string
	if err := WriteToolVersions(root, root, nil, func(s string) { warned = append(warned, s) }); err != nil {
		t.Fatal(err)
	}
	if len(warned) > 0 {
		t.Fatalf("warnings without asdf: %q", warned)
	}
	if _, err := os.Stat(filepath.Join(root, ".tool-versions")); err == nil {
		t.Fatal("file written without asdf")
	}
}

func TestWriteToolVersionsAtomicOverwrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "asdf"))
	mkInstalls(t, "golang", "1.26.1")
	ws := filepath.Join(root, "ws")
	os.MkdirAll(ws, 0o755)
	os.WriteFile(filepath.Join(ws, ".tool-versions"), []byte("python 1.0.0\n"), 0o644) // user file: overwritten
	if err := WriteToolVersions(ws, root, nil, nil); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(ws)
	if len(ents) != 1 || ents[0].Name() != ".tool-versions" {
		t.Fatalf("temp file left: %v", ents)
	}
	data, _ := os.ReadFile(filepath.Join(ws, ".tool-versions"))
	if !strings.HasPrefix(string(data), toolVersionsHeader) || !strings.Contains(string(data), "golang 1.26.1\n") {
		t.Fatalf("got %q", data)
	}
}

func TestWriteToolVersionsMalformedSetting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "asdf"))
	mkInstalls(t, "golang", "1.26.1")
	os.WriteFile(filepath.Join(root, ".workflow.json"), []byte("{bad"), 0o644)
	var warns []string
	if err := WriteToolVersions(root, root, nil, func(m string) { warns = append(warns, m) }); err != nil {
		t.Fatal(err)
	}
	if len(warns) == 0 || !strings.Contains(warns[0], ".workflow.json") {
		t.Fatalf("warnings %q", warns)
	}
}

func TestWriteToolVersionsGolangNoneWithGoMod(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "asdf"))
	mkInstalls(t, "golang", "1.26.1")
	mkInstalls(t, "python", "3.13.7")
	os.WriteFile(filepath.Join(root, ".workflow.json"), []byte(`{"toolVersions":{"golang":"none"}}`), 0o644)
	repo := mkRepo(t, root, "r", "module x\n\ngo 1.26.1\n")
	if err := WriteToolVersions(root, root, map[string]string{"r": repo}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".tool-versions"))
	if strings.Contains(string(data), "\ngolang ") || !strings.Contains(string(data), "python 3.13.7\n") {
		t.Fatalf("got %q", data)
	}
}

func TestWriteToolVersionsMalformedSettingNoAsdf(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ASDF_DATA_DIR", filepath.Join(root, "none"))
	os.WriteFile(filepath.Join(root, ".workflow.json"), []byte("{bad"), 0o644)
	var warns []string
	if err := WriteToolVersions(root, root, nil, func(m string) { warns = append(warns, m) }); err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], ".workflow.json") {
		t.Fatalf("warnings %q", warns)
	}
}
