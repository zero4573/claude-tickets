package tasksplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// release is what the fake server serves for version 9.9.9.
var release = map[string]string{
	"main.js":       "console.log('tasks')\n",
	"manifest.json": `{"id": "obsidian-tasks-plugin", "version": "9.9.9"}` + "\n",
	"styles.css":    ".tasks {}\n",
}

func hexSum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func testPin() Pin {
	p := Pin{ID: "obsidian-tasks-plugin", Repo: "acme/tasks", Version: "9.9.9", SHA256: map[string]string{}}
	for f, body := range release {
		p.SHA256[f] = hexSum(body)
	}
	return p
}

// server serves release under /9.9.9/<file>; fail maps a file to what's
// served instead ("404", "bad" for wrong bytes, "hang" to never answer in
// time). It counts the requests.
type server struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	fail  map[string]string
}

func newServer(t *testing.T, fail map[string]string) *server {
	s := &server{fail: fail}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		f := strings.TrimPrefix(r.URL.Path, "/9.9.9/")
		switch fail[f] {
		case "404":
			http.NotFound(w, r)
			return
		case "bad":
			_, _ = w.Write([]byte("tampered"))
			return
		case "hang":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		body, ok := release[f]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.paths)
}

func (s *server) installer() Installer {
	return Installer{Pin: testPin(), BaseURL: s.URL}
}

func vault(t *testing.T) (obs, plugin string) {
	obs = filepath.Join(t.TempDir(), ".obsidian")
	if err := os.MkdirAll(obs, 0o755); err != nil {
		t.Fatal(err)
	}
	return obs, filepath.Join(obs, "plugins", "obsidian-tasks-plugin")
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshot is every file under dir (relative path: content), with mtimes.
type snap map[string]string

func snapshot(t *testing.T, dir string) snap {
	t.Helper()
	s := snap{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if info.IsDir() {
			s[rel+"/"] = ""
			return nil
		}
		data, _ := os.ReadFile(p)
		s[rel] = string(data) + "@" + info.ModTime().String()
		return nil
	})
	return s
}

func (a snap) equal(b snap) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// leftovers is the ct temporary folders in obs.
func leftovers(t *testing.T, obs string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(obs, ".ct-tasks-*"))
	return m
}

// installed writes a plugin of version v (main.js, manifest.json), with
// mtimes in the past.
func installed(t *testing.T, plugin, manifest string) {
	t.Helper()
	write(t, filepath.Join(plugin, "main.js"), "old main\n")
	write(t, filepath.Join(plugin, "manifest.json"), manifest)
	write(t, filepath.Join(plugin, "data.json"), `{"mine": true}`)
	past := time.Now().Add(-48 * time.Hour)
	for _, f := range []string{"main.js", "manifest.json", "data.json"} {
		if err := os.Chtimes(filepath.Join(plugin, f), past, past); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEmbedded(t *testing.T) {
	p, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "obsidian-tasks-plugin" || p.Repo != "obsidian-tasks-group/obsidian-tasks" {
		t.Errorf("pin = %+v", p)
	}
	if _, ok := parseVersion(p.Version); !ok {
		t.Errorf("version %q doesn't parse", p.Version)
	}
	for _, f := range []string{"main.js", "manifest.json"} {
		if len(p.SHA256[f]) != 64 {
			t.Errorf("sha256 of %s = %q", f, p.SHA256[f])
		}
	}
	var settings map[string]any
	if err := json.Unmarshal(Settings(), &settings); err != nil {
		t.Fatalf("the settings aren't a JSON object: %v", err)
	}
	// The workflow's task lists need these
	if settings["taskFormat"] != "tasksPluginEmoji" || !strings.Contains(string(Settings()), `"symbol": "/"`) {
		t.Errorf("the settings lack the workflow's format or the In Progress status")
	}
	if got := (Installer{Pin: p}).URL("main.js"); got != "https://github.com/obsidian-tasks-group/obsidian-tasks/releases/download/"+p.Version+"/main.js" {
		t.Errorf("URL = %s", got)
	}
}

func TestParsePinRejects(t *testing.T) {
	good := `"id": "x", "repo": "a/b", "version": "1.2.3"`
	h := strings.Repeat("ab", 32)
	for name, body := range map[string]string{
		"not JSON":      `{`,
		"no id":         `{"repo": "a/b", "version": "1.2.3", "sha256": {"main.js": "` + h + `", "manifest.json": "` + h + `"}}`,
		"bad version":   `{"id": "x", "repo": "a/b", "version": "1.2", "sha256": {"main.js": "` + h + `", "manifest.json": "` + h + `"}}`,
		"no manifest":   `{` + good + `, "sha256": {"main.js": "` + h + `"}}`,
		"short hash":    `{` + good + `, "sha256": {"main.js": "abc", "manifest.json": "` + h + `"}}`,
		"not hex":       `{` + good + `, "sha256": {"main.js": "` + strings.Repeat("zz", 32) + `", "manifest.json": "` + h + `"}}`,
		"path for file": `{` + good + `, "sha256": {"main.js": "` + h + `", "manifest.json": "` + h + `", "../x": "` + h + `"}}`,
	} {
		if _, err := ParsePin([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p, err := ParsePin([]byte(`{` + good + `, "sha256": {"main.js": "` + strings.ToUpper(h) + `", "manifest.json": "` + h + `"}}`))
	if err != nil || p.SHA256["main.js"] != h {
		t.Errorf("upper-case hex: %v, %q", err, p.SHA256["main.js"])
	}
}

func TestFreshInstall(t *testing.T) {
	s := newServer(t, nil)
	obs, plugin := vault(t)
	out, ver, err := s.installer().Ensure(obs)
	if err != nil || out != Fresh || ver != "9.9.9" {
		t.Fatalf("Ensure = %v, %q, %v", out, ver, err)
	}
	for f, body := range release {
		data, err := os.ReadFile(filepath.Join(plugin, f))
		if err != nil || string(data) != body {
			t.Errorf("%s = %q, %v", f, data, err)
		}
	}
	if st, v, err := Inspect(plugin); st != Installed || v != "9.9.9" || err != nil {
		t.Errorf("Inspect = %v, %q, %v", st, v, err)
	}
	if st, err := os.Stat(plugin); err != nil || st.Mode().Perm() != 0o755 {
		t.Errorf("plugin dir mode: %v, %v", st.Mode(), err)
	}
	if l := leftovers(t, obs); len(l) > 0 {
		t.Errorf("left over: %v", l)
	}
	if n := s.requests(); n != 3 {
		t.Errorf("%d requests", n)
	}

	// Re-run: nothing downloaded or rewritten
	before := snapshot(t, plugin)
	out, ver, err = s.installer().Ensure(obs)
	if err != nil || out != Current || ver != "9.9.9" {
		t.Fatalf("re-run: Ensure = %v, %q, %v", out, ver, err)
	}
	if n := s.requests(); n != 3 {
		t.Errorf("re-run downloaded: %d requests", n)
	}
	if !snapshot(t, plugin).equal(before) {
		t.Error("re-run changed the plugin dir")
	}
}

func TestFailuresLeaveThePluginDirAlone(t *testing.T) {
	cases := map[string]struct {
		fail map[string]string
		want string
	}{
		"main.js tampered":       {map[string]string{"main.js": "bad"}, "main.js: sha256 mismatch"},
		"manifest.json tampered": {map[string]string{"manifest.json": "bad"}, "manifest.json: sha256 mismatch"},
		"styles.css tampered":    {map[string]string{"styles.css": "bad"}, "styles.css: sha256 mismatch"},
		"second file 404":        {map[string]string{"manifest.json": "404"}, "manifest.json: HTTP 404"},
		"third file 404":         {map[string]string{"styles.css": "404"}, "styles.css: HTTP 404"},
	}
	for name, c := range cases {
		for _, partial := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "", true: " (data.json only)"}[partial], func(t *testing.T) {
				s := newServer(t, c.fail)
				obs, plugin := vault(t)
				if partial {
					write(t, filepath.Join(plugin, "data.json"), `{"mine": true}`)
				}
				before := snapshot(t, obs)
				out, _, err := s.installer().Ensure(obs)
				if out != NotInstalled || err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("Ensure = %v, %v; want an error with %q", out, err, c.want)
				}
				if after := snapshot(t, obs); !after.equal(before) {
					t.Errorf(".obsidian changed:\nbefore %v\nafter  %v", before, after)
				}
			})
		}
	}
}

func TestInstallErrorLeavesNothing(t *testing.T) {
	t.Run("plugins is a file", func(t *testing.T) {
		s := newServer(t, nil)
		obs, _ := vault(t)
		write(t, filepath.Join(obs, "plugins"), "")
		before := snapshot(t, obs)
		out, _, err := s.installer().Ensure(obs)
		if out != NotInstalled || err == nil {
			t.Fatalf("Ensure = %v, %v", out, err)
		}
		if !snapshot(t, obs).equal(before) {
			t.Error(".obsidian changed")
		}
	})
	// A write fails partway through staging (the second file)
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	for _, partial := range []bool{false, true} {
		n := 0
		writeFile = func(p string, data []byte) error {
			if n++; n == 2 {
				return errors.New("disk full")
			}
			return orig(p, data)
		}
		s := newServer(t, nil)
		obs, plugin := vault(t)
		if partial {
			write(t, filepath.Join(plugin, "data.json"), `{"mine": true}`)
		}
		before := snapshot(t, obs)
		out, _, err := s.installer().Ensure(obs)
		if out != NotInstalled || err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("partial %t: Ensure = %v, %v", partial, out, err)
		}
		if after := snapshot(t, obs); !after.equal(before) {
			t.Errorf("partial %t: .obsidian changed:\nbefore %v\nafter  %v", partial, before, after)
		}
	}
}

func TestTimeout(t *testing.T) {
	s := newServer(t, map[string]string{"main.js": "hang"})
	obs, plugin := vault(t)
	in := s.installer()
	in.Timeout = 100 * time.Millisecond
	start := time.Now()
	out, _, err := in.Ensure(obs)
	if out != NotInstalled || err == nil || !strings.Contains(err.Error(), "main.js:") {
		t.Fatalf("Ensure = %v, %v", out, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
	if _, err := os.Stat(plugin); err == nil {
		t.Error("plugin dir created")
	}
}

func TestInstalledIsKept(t *testing.T) {
	cases := map[string]struct {
		manifest string
		out      Outcome
		ver      string
	}{
		"same":        {`{"version": "9.9.9"}`, Current, "9.9.9"},
		"newer":       {`{"version": "9.10.0"}`, KeptNewer, "9.10.0"},
		"older":       {`{"version": "9.3.0"}`, KeptOlder, "9.3.0"},
		"no version":  {`{"id": "obsidian-tasks-plugin"}`, KeptUnknown, ""},
		"bad version": {`{"version": "x"}`, KeptUnknown, "x"},
		"not JSON":    {`{version`, KeptUnknown, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, nil)
			obs, plugin := vault(t)
			installed(t, plugin, c.manifest)
			before := snapshot(t, plugin)
			out, ver, err := s.installer().Ensure(obs)
			if out != c.out || ver != c.ver || (err != nil) != (c.out == KeptUnknown) {
				t.Errorf("Ensure = %v, %q, %v; want %v, %q", out, ver, err, c.out, c.ver)
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

func TestPartialInstall(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"data.json only":           {"data.json": `{"mine": true}`},
		"main.js without manifest": {"main.js": "half", "data.json": `{"mine": true}`},
	} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, nil)
			obs, plugin := vault(t)
			for f, body := range files {
				write(t, filepath.Join(plugin, f), body)
			}
			out, _, err := s.installer().Ensure(obs)
			if out != Fresh || err != nil {
				t.Fatalf("Ensure = %v, %v", out, err)
			}
			for f, body := range release {
				if data, _ := os.ReadFile(filepath.Join(plugin, f)); string(data) != body {
					t.Errorf("%s = %q", f, data)
				}
			}
			if data, _ := os.ReadFile(filepath.Join(plugin, "data.json")); string(data) != `{"mine": true}` {
				t.Errorf("data.json = %q", data)
			}
			if l := leftovers(t, obs); len(l) > 0 {
				t.Errorf("left over: %v", l)
			}
		})
	}
}

func TestLeftoversRemoved(t *testing.T) {
	s := newServer(t, nil)
	obs, plugin := vault(t)
	installed(t, plugin, `{"version": "9.9.9"}`)
	write(t, filepath.Join(obs, ".ct-tasks-staging-x", "main.js"), "x")
	write(t, filepath.Join(obs, ".ct-tasks-old-x", "main.js"), "x")
	if _, _, err := s.installer().Ensure(obs); err != nil {
		t.Fatal(err)
	}
	if l := leftovers(t, obs); len(l) > 0 {
		t.Errorf("left over: %v", l)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"8.10.0", "8.4.0", 1, true},
		{"8.3.0", "8.4.0", -1, true},
		{"9.0.0", "8.40.10", 1, true},
		{"v8.4.0", "8.4.0", 0, true},
		{"8.4.0+build.5", "8.4.0", 0, true},
		{"8.4.0-beta.1", "8.4.0", -1, true},
		{"8.4.0", "8.4.0-beta.1", 1, true},
		{"8.4.0-beta.1", "8.4.0-beta.2", -1, true},
		{"8.4", "8.4.0", 0, false},
		{"", "8.4.0", 0, false},
		{"abc", "8.4.0", 0, false},
		{"8.4.0-", "8.4.0", 0, false},
		{"8.-4.0", "8.4.0", 0, false},
		{"8.4.0", "8.4.x", 0, false},
	} {
		got, ok := CompareVersions(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("CompareVersions(%q, %q) = %d, %t; want %d, %t", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestEnable(t *testing.T) {
	const id = "obsidian-tasks-plugin"
	for name, c := range map[string]struct {
		before string // "-": no file
		want   []string
	}{
		"missing":   {"-", []string{id}},
		"empty":     {"", []string{id}},
		"blank":     {" \n", []string{id}},
		"[]":        {"[]", []string{id}},
		"others":    {`["a","b"]`, []string{"a", "b", id}},
		"listed":    {`["a","` + id + `"]`, nil},
		"object":    {`{}`, nil},
		"null":      {`null`, nil},
		"not JSON":  {`[`, nil},
		"a number":  {`1`, nil},
		"duplicate": {`["` + id + `", "` + id + `"]`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			obs, _ := vault(t)
			file := filepath.Join(obs, "community-plugins.json")
			if c.before != "-" {
				write(t, file, c.before)
				past := time.Now().Add(-time.Hour)
				_ = os.Chtimes(file, past, past)
			}
			before := snapshot(t, obs)
			added, err := Enable(obs, id)
			if c.want == nil {
				if added {
					t.Error("added")
				}
				wantErr := !strings.Contains(c.before, id)
				if wantErr != errors.Is(err, ErrNotArray) {
					t.Errorf("err = %v", err)
				}
				if !snapshot(t, obs).equal(before) {
					t.Error("the file changed")
				}
				return
			}
			if !added || err != nil {
				t.Fatalf("Enable = %t, %v", added, err)
			}
			var got []string
			data, _ := os.ReadFile(file)
			if err := json.Unmarshal(data, &got); err != nil || strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("file = %s (%v)", data, err)
			}
			if l, _ := filepath.Glob(file + ".tmp"); len(l) > 0 {
				t.Error("left a .tmp file")
			}
		})
	}
}

func TestEnsureSettings(t *testing.T) {
	settings := Settings()
	for name, c := range map[string]struct {
		before string // "-": no file
		wrote  bool
	}{
		"missing":  {"-", true},
		"empty":    {"", true},
		"blank":    {"\n  \n", true},
		"existing": {`{"taskFormat": "dataview"}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, plugin := vault(t)
			if err := os.MkdirAll(plugin, 0o755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(plugin, "data.json")
			if c.before != "-" {
				write(t, file, c.before)
			}
			wrote, err := EnsureSettings(plugin, settings)
			if err != nil || wrote != c.wrote {
				t.Fatalf("EnsureSettings = %t, %v", wrote, err)
			}
			data, _ := os.ReadFile(file)
			want := string(settings)
			if !c.wrote {
				want = c.before
			}
			if string(data) != want {
				t.Errorf("data.json = %q", data)
			}
			entries, _ := os.ReadDir(plugin)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			if strings.Join(names, ",") != "data.json" {
				t.Errorf("plugin dir holds %v", names)
			}
		})
	}
}
