package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirsFor(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		goos string
		env  map[string]string
		home string
		want Dirs
	}{
		{"linux", nil, "/home/jane", Dirs{"/home/jane/.config/tix", "/home/jane/.cache/tix", "/home/jane/.local/state"}},
		{"linux", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_CACHE_HOME": "/x/cache", "XDG_STATE_HOME": "/x/state"}, "/home/jane",
			Dirs{"/x/cfg/tix", "/x/cache/tix", "/x/state"}},
		{"darwin", nil, "/Users/jane", Dirs{"/Users/jane/Library/Application Support/tix", "/Users/jane/Library/Caches/tix", "/Users/jane/Library/Logs/tix"}},
		{"darwin", map[string]string{"XDG_CONFIG_HOME": "/Users/jane/.config"}, "/Users/jane",
			Dirs{"/Users/jane/.config/tix", "/Users/jane/Library/Caches/tix", "/Users/jane/Library/Logs/tix"}},
		{"windows", map[string]string{"AppData": `C:\Users\jane\AppData\Roaming`, "LocalAppData": `C:\Users\jane\AppData\Local`}, `C:\Users\jane`,
			Dirs{`C:\Users\jane\AppData\Roaming\tix`, `C:\Users\jane\AppData\Local\tix\cache`, `C:\Users\jane\AppData\Local\tix\logs`}},
		{"windows", nil, `C:\Users\jane`,
			Dirs{`C:\Users\jane\AppData\Roaming\tix`, `C:\Users\jane\AppData\Local\tix\cache`, `C:\Users\jane\AppData\Local\tix\logs`}},
		{"freebsd", map[string]string{"TIX_CONFIG_DIR": "/c", "TIX_CACHE": "/k", "TIX_STATE_DIR": "/s", "XDG_CONFIG_HOME": "/ignored"}, "/home/jane",
			Dirs{"/c", "/k", "/s"}},
	} {
		if got := DirsFor(tc.goos, env(tc.env), tc.home); got != tc.want {
			t.Errorf("%s %v:\n got %+v\nwant %+v", tc.goos, tc.env, got, tc.want)
		}
	}
}

func TestLegacyDirsFor(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		goos string
		env  map[string]string
		home string
		want Dirs
	}{
		// linux logs are ~/.local/state, not tix's own: nothing to move
		{"linux", nil, "/home/jane", Dirs{"/home/jane/.config/claude-tickets", "/home/jane/.cache/claude-tickets", ""}},
		{"darwin", nil, "/Users/jane", Dirs{"/Users/jane/Library/Application Support/claude-tickets", "/Users/jane/Library/Caches/claude-tickets", "/Users/jane/Library/Logs/claude-tickets"}},
		{"windows", nil, `C:\Users\jane`,
			Dirs{`C:\Users\jane\AppData\Roaming\claude-tickets`, `C:\Users\jane\AppData\Local\claude-tickets\cache`, `C:\Users\jane\AppData\Local\claude-tickets\logs`}},
		// the TIX_* overrides don't say where the old install was
		{"linux", map[string]string{"TIX_CONFIG_DIR": "/c", "XDG_CACHE_HOME": "/x"}, "/home/jane", Dirs{"/home/jane/.config/claude-tickets", "/x/claude-tickets", ""}},
	} {
		if got := LegacyDirsFor(tc.goos, env(tc.env), tc.home); got != tc.want {
			t.Errorf("%s %v:\n got %+v\nwant %+v", tc.goos, tc.env, got, tc.want)
		}
	}
}

func TestMigrateLegacy(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	mk := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// the config moves; the cache stays where tix's exists already
	mk(filepath.Join(home, ".config", "claude-tickets"))
	if err := os.WriteFile(filepath.Join(home, ".config", "claude-tickets", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(home, ".cache", "claude-tickets"))
	mk(filepath.Join(home, ".cache", "tix"))
	got := MigrateLegacy("linux", getenv, home)
	if len(got) != 1 || got[0].Err != nil || got[0].To != filepath.Join(home, ".config", "tix") {
		t.Fatalf("MigrateLegacy = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "tix", "config.json")); err != nil {
		t.Errorf("config not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "claude-tickets")); err != nil {
		t.Errorf("the old cache went though tix's existed: %v", err)
	}
	// nothing left to do; a TIX_* override leaves its folder alone
	if got := MigrateLegacy("linux", getenv, home); len(got) != 0 {
		t.Errorf("second run = %+v", got)
	}
	mk(filepath.Join(home, ".config", "claude-tickets"))
	env["TIX_CONFIG_DIR"] = filepath.Join(home, "elsewhere")
	if got := MigrateLegacy("linux", getenv, home); len(got) != 0 {
		t.Errorf("with TIX_CONFIG_DIR = %+v", got)
	}
}
