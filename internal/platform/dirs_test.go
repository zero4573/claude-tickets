package platform

import "testing"

func TestDirsFor(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		goos string
		env  map[string]string
		home string
		want Dirs
	}{
		{"linux", nil, "/home/jane", Dirs{"/home/jane/.config/claude-tickets", "/home/jane/.cache/claude-tickets", "/home/jane/.local/state"}},
		{"linux", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "XDG_CACHE_HOME": "/x/cache", "XDG_STATE_HOME": "/x/state"}, "/home/jane",
			Dirs{"/x/cfg/claude-tickets", "/x/cache/claude-tickets", "/x/state"}},
		{"darwin", nil, "/Users/jane", Dirs{"/Users/jane/Library/Application Support/claude-tickets", "/Users/jane/Library/Caches/claude-tickets", "/Users/jane/Library/Logs/claude-tickets"}},
		{"darwin", map[string]string{"XDG_CONFIG_HOME": "/Users/jane/.config"}, "/Users/jane",
			Dirs{"/Users/jane/.config/claude-tickets", "/Users/jane/Library/Caches/claude-tickets", "/Users/jane/Library/Logs/claude-tickets"}},
		{"windows", map[string]string{"AppData": `C:\Users\jane\AppData\Roaming`, "LocalAppData": `C:\Users\jane\AppData\Local`}, `C:\Users\jane`,
			Dirs{`C:\Users\jane\AppData\Roaming\claude-tickets`, `C:\Users\jane\AppData\Local\claude-tickets\cache`, `C:\Users\jane\AppData\Local\claude-tickets\logs`}},
		{"windows", nil, `C:\Users\jane`,
			Dirs{`C:\Users\jane\AppData\Roaming\claude-tickets`, `C:\Users\jane\AppData\Local\claude-tickets\cache`, `C:\Users\jane\AppData\Local\claude-tickets\logs`}},
		{"freebsd", map[string]string{"CLAUDE_TICKETS_CONFIG_DIR": "/c", "CLAUDE_TICKETS_CACHE": "/k", "CLAUDE_TICKETS_STATE_DIR": "/s", "XDG_CONFIG_HOME": "/ignored"}, "/home/jane",
			Dirs{"/c", "/k", "/s"}},
	} {
		if got := DirsFor(tc.goos, env(tc.env), tc.home); got != tc.want {
			t.Errorf("%s %v:\n got %+v\nwant %+v", tc.goos, tc.env, got, tc.want)
		}
	}
}
