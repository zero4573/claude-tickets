package tasksplugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A mirror URL works with or without a trailing slash, and under a path.
func TestMirrorURL(t *testing.T) {
	p := testPin()
	for base, want := range map[string]string{
		"https://m.example/tasks":   "https://m.example/tasks/9.9.9/main.js",
		"https://m.example/tasks/":  "https://m.example/tasks/9.9.9/main.js",
		"https://m.example/tasks//": "https://m.example/tasks/9.9.9/main.js",
		"":                          "https://github.com/acme/tasks/releases/download/9.9.9/main.js",
	} {
		if got := (Installer{Pin: p, BaseURL: base}).URL("main.js"); got != want {
			t.Errorf("URL with base %q = %s, want %s", base, got, want)
		}
	}
	// End to end through a trailing-slash base
	s := newServer(t, nil)
	obs, plugin := vault(t)
	in := s.installer()
	in.BaseURL += "/"
	if out, _, err := in.Ensure(obs); out != Fresh || err != nil {
		t.Fatalf("Ensure = %v, %v", out, err)
	}
	for f, body := range release {
		if data, _ := os.ReadFile(filepath.Join(plugin, f)); string(data) != body {
			t.Errorf("%s = %q", f, data)
		}
	}
}

// Versions the original cases don't cover: a v prefix, a prerelease of the
// pin (older), a number instead of a string (unknown). None downloads or
// writes anything.
func TestInstalledVersionForms(t *testing.T) {
	for name, c := range map[string]struct {
		manifest string
		out      Outcome
	}{
		"v prefix":          {`{"version": "v9.9.9"}`, Current},
		"prerelease of pin": {`{"version": "9.9.9-beta.1"}`, KeptOlder},
		"build metadata":    {`{"version": "9.9.9+abc"}`, Current},
		"number":            {`{"version": 9}`, KeptUnknown},
		"two parts":         {`{"version": "9.9"}`, KeptUnknown},
		"empty":             {`{"version": ""}`, KeptUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, nil)
			obs, plugin := vault(t)
			installed(t, plugin, c.manifest)
			before := snapshot(t, plugin)
			out, _, err := s.installer().Ensure(obs)
			if out != c.out || (err != nil) != (c.out == KeptUnknown) {
				t.Errorf("Ensure = %v, %v; want %v", out, err, c.out)
			}
			if n := s.requests(); n != 0 {
				t.Errorf("%d requests", n)
			}
			if !snapshot(t, plugin).equal(before) {
				t.Error("the plugin dir changed")
			}
		})
	}
}

// The embedded pin installs the three release assets the Nix module did.
func TestEmbeddedPinHasTheThreeAssets(t *testing.T) {
	p, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Files(), ","); got != "main.js,manifest.json,styles.css" {
		t.Errorf("files = %s", got)
	}
}

// One source of truth (AC 14/18): the Nix module reads the shared pin and
// settings rather than keeping its own copies. Skipped where nix/ isn't in
// the source tree (the Nix build's fileset leaves it out).
func TestNixModuleUsesTheSharedPin(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "nix", "obsidian.nix"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("nix/obsidian.nix isn't in this source tree")
	}
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"builtins.fromJSON (builtins.readFile ../assets/obsidian/tasks-plugin.json)",
		"${../assets/obsidian/tasks-settings.json}",
		"tasksPin.sha256.${name}",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("nix/obsidian.nix doesn't contain %q", want)
		}
	}
	for _, stale := range []string{"tasksVersion =", "../obsidian/tasks-settings.json"} {
		if strings.Contains(src, stale) {
			t.Errorf("nix/obsidian.nix still has %q (a second pin or the old settings path)", stale)
		}
	}
}

// Two runs at once on a fresh vault: the plugin ends up installed, whole
// and correct, with no temporary folders left, whichever run wins.
func TestConcurrentEnsure(t *testing.T) {
	s := newServer(t, nil)
	obs, plugin := vault(t)
	var wg sync.WaitGroup
	outs := make([]Outcome, 4)
	for i := range outs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i], _, _ = s.installer().Ensure(obs)
		}(i)
	}
	wg.Wait()
	fresh := 0
	for _, o := range outs {
		if o == Fresh {
			fresh++
		}
	}
	if fresh == 0 {
		t.Errorf("no run installed it: %v", outs)
	}
	for f, body := range release {
		if data, _ := os.ReadFile(filepath.Join(plugin, f)); string(data) != body {
			t.Errorf("%s = %q", f, data)
		}
	}
	entries, _ := os.ReadDir(plugin)
	if len(entries) != len(release) {
		t.Errorf("plugin dir has %d entries", len(entries))
	}
	if l := leftovers(t, obs); len(l) > 0 {
		t.Errorf("leftovers: %v", l)
	}
}

// A data.json.tmp left by an interrupted EnsureSettings doesn't stop the
// next run, and isn't left behind.
func TestEnsureSettingsOverStaleTmp(t *testing.T) {
	_, plugin := vault(t)
	write(t, filepath.Join(plugin, "data.json.tmp"), "half")
	wrote, err := EnsureSettings(plugin, []byte(`{"a":1}`))
	if !wrote || err != nil {
		t.Fatalf("EnsureSettings = %t, %v", wrote, err)
	}
	if data, _ := os.ReadFile(filepath.Join(plugin, "data.json")); string(data) != `{"a":1}` {
		t.Errorf("data.json = %q", data)
	}
	if _, err := os.Stat(filepath.Join(plugin, "data.json.tmp")); err == nil {
		t.Error("data.json.tmp left behind")
	}
}
