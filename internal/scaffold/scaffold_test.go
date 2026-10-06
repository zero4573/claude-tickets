package scaffold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zero4573/claude-tickets/assets"
)

func TestNorm(t *testing.T) {
	for in, want := range map[string]string{
		"":            "",
		"\n\n":        "",
		"a":           "a\n",
		"a\n":         "a\n",
		"a\n\n":       "a\n",
		"a\r\nb\r\n":  "a\nb\n",
		"a\rb\n":      "a\rb\n", // a lone \r is content
		"a\r\n\r\n\n": "a\n",
	} {
		if got := string(Norm([]byte(in))); got != want {
			t.Errorf("Norm(%q) = %q, want %q", in, got, want)
		}
	}
	if Hash([]byte("a")) != Hash([]byte("a\n")) || Hash([]byte("a\n\n")) != Hash([]byte("a\r\n")) {
		t.Error("a, a\\n, a\\n\\n and a\\r\\n should hash the same")
	}
	if Hash([]byte("a\rb")) == Hash([]byte("ab")) {
		t.Error("a lone \\r should count")
	}
}

func TestHistoryCoversScaffold(t *testing.T) {
	src, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Files) == 0 {
		t.Fatal("no scaffold files")
	}
	for _, f := range src.Files {
		if !contains(src.History[f.Rel], f.Hash) {
			t.Errorf("%s: its current version isn't in assets/scaffold-history.json: run go generate ./assets", f.Rel)
		}
	}
}

func TestHistoryFile(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(assets.ScaffoldHistory, &raw); err != nil {
		t.Fatal("not valid JSON:", err)
	}
	hist, err := ParseHistory(assets.ScaffoldHistory)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := FormatHistory(hist)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(formatted, assets.ScaffoldHistory) {
		t.Error("assets/scaffold-history.json isn't as genhistory writes it (sorted, unique): run go generate ./assets")
	}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for rel, hashes := range hist {
		if rel == Skipped {
			t.Errorf("%s is never shipped, so it has no history", rel)
		}
		if !sort.StringsAreSorted(hashes) {
			t.Errorf("%s: hashes not sorted", rel)
		}
		seen := map[string]bool{}
		for _, h := range hashes {
			if !hex.MatchString(h) || seen[h] {
				t.Errorf("%s: bad or repeated hash %q", rel, h)
			}
			seen[h] = true
		}
	}
	files, err := FromFS(assets.Scaffold, "vault-scaffold")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if len(hist[f.Rel]) == 0 {
			t.Errorf("%s: no history", f.Rel)
		}
	}
}

// source is a scaffold of the given files; hist adds older versions.
func source(t *testing.T, files map[string]string, hist map[string][]string) Source {
	t.Helper()
	m := fstest.MapFS{"s/" + Skipped: {Data: []byte("{}")}}
	for rel, data := range files {
		m["s/"+rel] = &fstest.MapFile{Data: []byte(data)}
	}
	fl, err := FromFS(m, "s")
	if err != nil {
		t.Fatal(err)
	}
	h := map[string][]string{}
	for _, f := range fl {
		h[f.Rel] = append(h[f.Rel], f.Hash)
	}
	for rel, versions := range hist {
		for _, v := range versions {
			h[rel] = append(h[rel], Hash([]byte(v)))
		}
	}
	return Source{Files: fl, History: h}
}

func newVault(t *testing.T, files map[string]string) string {
	t.Helper()
	v := t.TempDir()
	if err := os.Mkdir(filepath.Join(v, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		write(t, v, rel, data)
	}
	return v
}

func write(t *testing.T, v, rel, data string) {
	t.Helper()
	p := filepath.Join(v, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, v, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(v, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func states(entries []Entry) map[string]State {
	m := map[string]State{}
	for _, e := range entries {
		m[e.Rel] = e.State
	}
	return m
}

func TestClassify(t *testing.T) {
	src := source(t, map[string]string{
		"missing.md":   "new\n",
		"current.md":   "new\n",
		"crlf.md":      "a\nb\n",
		"stale.md":     "new\n",
		"recorded.md":  "new\n",
		"edited.md":    "new\n",
		"dir.md":       "new\n",
		"t/nested.md":  "new\n",
		"t/edited2.md": "new\n",
	}, map[string][]string{
		"stale.md":       {"old\n"},
		"gone.md":        {"gone\n"},
		"gone-edited.md": {"gone\n"},
		"gone-absent.md": {"gone\n"},
	})
	v := newVault(t, map[string]string{
		"current.md":       "new",
		"crlf.md":          "a\r\nb\r\n",
		"stale.md":         "old\n",
		"recorded.md":      "mine, recorded\n",
		"edited.md":        "mine\n",
		"t/nested.md":      "new\n",
		"t/edited2.md":     "old\n", // an old version of another file
		"gone.md":          "gone\n",
		"gone-edited.md":   "gone, edited\n",
		"gone-recorded.md": "recorded\n",
	})
	if err := os.Mkdir(filepath.Join(v, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := Record{Files: map[string]Shipped{
		"recorded.md":      {SHA256: Hash([]byte("mine, recorded\n"))},
		"stale.md":         {SHA256: Hash([]byte("old\n"))}, // recorded and known: stale
		"gone-recorded.md": {SHA256: Hash([]byte("recorded\n"))},
		"../outside.md":    {SHA256: Hash([]byte("x"))},
	}}
	entries, err := Classify(v, src, rec)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]State{
		"missing.md":       Missing,
		"current.md":       UpToDate,
		"crlf.md":          UpToDate,
		"stale.md":         Stale,
		"recorded.md":      Newer, // recorded, unknown to this ct: a newer ct wrote it
		"edited.md":        Edited,
		"dir.md":           Edited,
		"t/nested.md":      UpToDate,
		"t/edited2.md":     Edited,
		"gone.md":          RetiredClean,
		"gone-edited.md":   RetiredEdited,
		"gone-recorded.md": Newer,
	}
	got := states(entries)
	for rel, s := range want {
		if got[rel] != s {
			t.Errorf("%s: state %d, want %d", rel, got[rel], s)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v (absent retired files and paths outside the vault aren't listed)", got, want)
	}
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel }) {
		t.Error("entries not sorted")
	}
	for _, e := range entries {
		if e.Rel == "dir.md" && e.Reason != "not a regular file" {
			t.Errorf("dir.md: reason %q", e.Reason)
		}
		if strings.HasPrefix(e.Rel, "gone") && e.Ship != nil {
			t.Errorf("%s: retired with a shipped file", e.Rel)
		}
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink("current.md", filepath.Join(v, "missing.md")); err != nil {
			t.Fatal(err)
		}
		entries, err := Classify(v, src, rec)
		if err != nil {
			t.Fatal(err)
		}
		if s := states(entries)["missing.md"]; s != Edited {
			t.Errorf("a symlink: state %d, want Edited", s)
		}
	}
}

type snap map[string]string

// snapshot is every path of a vault, with the mode, mtime and size of files.
func snapshot(t *testing.T, v string) snap {
	t.Helper()
	s := snap{}
	err := filepath.WalkDir(v, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(v, p)
		if d.IsDir() {
			// taking and releasing the lock changes the root's mtime
			s[rel] = "dir"
		} else {
			s[rel] = fmt.Sprint(info.Mode(), info.ModTime(), info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var old = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func age(t *testing.T, v string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if err := os.Chtimes(filepath.Join(v, filepath.FromSlash(rel)), old, old); err != nil {
			t.Fatal(err)
		}
	}
}

func mtime(t *testing.T, v, rel string) time.Time {
	t.Helper()
	st, err := os.Stat(filepath.Join(v, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return st.ModTime()
}

func loadRec(t *testing.T, v string) Record {
	t.Helper()
	rec, state, w := loadRecord(v)
	if state != recordOK {
		t.Fatalf("record: state %d (%s)", state, w)
	}
	return rec
}

func applySetup(t *testing.T) (Source, string) {
	src := source(t, map[string]string{
		"missing.md":   "new\r\n", // written as shipped, not normalized
		"current.md":   "new\n",
		"stale.md":     "new\n",
		"edited.md":    "new\n",
		"t/missing.md": "new\n",
	}, map[string][]string{"stale.md": {"old\n"}, "gone.md": {"gone\n"}})
	v := newVault(t, map[string]string{
		"current.md": "new\n",
		"stale.md":   "old\n",
		"edited.md":  "mine\n",
		"gone.md":    "gone\n",
	})
	age(t, v, "current.md", "stale.md", "edited.md", "gone.md")
	return src, v
}

func TestApply(t *testing.T) {
	src, v := applySetup(t)
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"missing.md": Added, "t/missing.md": Added, "stale.md": Updated}; !sameDone(res.Done, want) {
		t.Errorf("done %v, want %v", res.Done, want)
	}
	if read(t, v, "missing.md") != "new\r\n" || read(t, v, "stale.md") != "new\n" || read(t, v, "t/missing.md") != "new\n" {
		t.Error("not written with the shipped bytes")
	}
	if read(t, v, "edited.md") != "mine\n" || read(t, v, "gone.md") != "gone\n" {
		t.Error("an edited or retired file was changed")
	}
	for _, rel := range []string{"current.md", "edited.md", "gone.md"} {
		if !mtime(t, v, rel).Equal(old) {
			t.Errorf("%s: written", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(v, ".vault.lock.d")); err == nil {
		t.Error("the lock wasn't released")
	}
	tmps, _ := filepath.Glob(filepath.Join(v, ".*.ct-tmp"))
	if len(tmps) > 0 {
		t.Error("temp files left:", tmps)
	}

	rec := loadRec(t, v)
	if rec.Format != RecordFormat || !strings.Contains(rec.Comment, "ct vault update") {
		t.Errorf("record header: %+v", rec)
	}
	newHash := Hash([]byte("new\n"))
	want := map[string]Shipped{
		"missing.md":   {newHash, "1.0"},
		"t/missing.md": {newHash, "1.0"},
		"current.md":   {newHash, "1.0"},
		"stale.md":     {newHash, "1.0"},
	}
	if !sameRecord(rec.Files, want) {
		t.Errorf("record %v, want %v", rec.Files, want)
	}

	// Again, with a newer ct: nothing to do, the record isn't rewritten
	age(t, v, RecordName)
	res, err = Apply(v, src, Options{Version: "2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Done) != 0 {
		t.Errorf("second run did %v", res.Done)
	}
	if !mtime(t, v, RecordName).Equal(old) {
		t.Error("record rewritten by a run that changed nothing")
	}

	// A file changing again gets the new version; the others keep theirs
	src2 := source(t, map[string]string{
		"missing.md": "new\r\n", "current.md": "newer\n", "stale.md": "new\n", "edited.md": "new\n", "t/missing.md": "new\n",
	}, map[string][]string{"current.md": {"new\n"}})
	if _, err := Apply(v, src2, Options{Version: "3.0"}); err != nil {
		t.Fatal(err)
	}
	rec = loadRec(t, v)
	if rec.Files["current.md"] != (Shipped{Hash([]byte("newer\n")), "3.0"}) || rec.Files["stale.md"].Version != "1.0" {
		t.Errorf("record %v", rec.Files)
	}
}

func sameDone(a, b map[string]string) bool {
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

func TestApplyKeepsEntries(t *testing.T) {
	src, v := applySetup(t)
	// edited.md was shipped once (its entry stays), retired.md too, while
	// it exists; vanished.md is neither shipped nor present (dropped)
	prev := map[string]Shipped{
		"edited.md":   {Hash([]byte("base\n")), "0.9"},
		"gone.md":     {Hash([]byte("gone\n")), "0.9"},
		"vanished.md": {Hash([]byte("x\n")), "0.9"},
	}
	if err := saveRecord(v, prev); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, src, Options{Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	rec := loadRec(t, v)
	if rec.Files["edited.md"] != prev["edited.md"] || rec.Files["gone.md"] != prev["gone.md"] {
		t.Errorf("record %v", rec.Files)
	}
	if _, ok := rec.Files["vanished.md"]; ok {
		t.Error("an entry for a path that's gone was kept")
	}
}

func TestApplyAddOnly(t *testing.T) {
	src, v := applySetup(t)
	res, err := Apply(v, src, Options{Version: "1.0", AddOnly: true, Owner: "ct-vault-init"})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"missing.md": Added, "t/missing.md": Added}; !sameDone(res.Done, want) {
		t.Errorf("done %v, want %v", res.Done, want)
	}
	if res.Count(Stale) != 1 || res.Count(Edited) != 1 {
		t.Errorf("stale %d, edited %d", res.Count(Stale), res.Count(Edited))
	}
	if read(t, v, "stale.md") != "old\n" || !mtime(t, v, "stale.md").Equal(old) {
		t.Error("AddOnly updated a stale file")
	}
	// recorded with the version it has
	if loadRec(t, v).Files["stale.md"].SHA256 != Hash([]byte("old\n")) {
		t.Error("a stale file under AddOnly isn't recorded with its own hash")
	}
}

func TestApplyDryRun(t *testing.T) {
	src, v := applySetup(t)
	before := snapshot(t, v)
	res, err := Apply(v, src, Options{DryRun: true, Take: []string{"edited.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"missing.md": Added, "t/missing.md": Added, "stale.md": Updated, "edited.md": Taken}; !sameDone(res.Done, want) {
		t.Errorf("done %v, want %v", res.Done, want)
	}
	after := snapshot(t, v)
	if len(before) != len(after) {
		t.Fatalf("paths changed:\n%v\n%v", before, after)
	}
	for p, s := range before {
		if after[p] != s {
			t.Errorf("%s changed", p)
		}
	}
}

func TestApplyTake(t *testing.T) {
	src, v := applySetup(t)
	res, err := Apply(v, src, Options{Version: "1.0", Take: []string{"edited.md", "current.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done["edited.md"] != Taken || res.Done["current.md"] != "" {
		t.Errorf("done %v", res.Done)
	}
	if read(t, v, "edited.md.bak") != "mine\n" || read(t, v, "edited.md") != "new\n" {
		t.Error("take: wrong contents")
	}
	if loadRec(t, v).Files["edited.md"].SHA256 != Hash([]byte("new\n")) {
		t.Error("take: not recorded")
	}

	// Refused: the .bak exists, not shipped, retired, not a regular file
	write(t, v, "edited.md", "mine again\n")
	for _, tc := range []struct{ take, err string }{
		{"edited.md", "edited.md.bak exists; move it away first"},
		{"nope.md", "not a file ct ships: nope.md"},
		{"gone.md", "gone.md isn't shipped any more"},
	} {
		before := snapshot(t, v)
		_, err := Apply(v, src, Options{Take: []string{tc.take}})
		if err == nil || err.Error() != tc.err {
			t.Errorf("take %s: error %v, want %q", tc.take, err, tc.err)
		}
		after := snapshot(t, v)
		for p, s := range before {
			if after[p] != s {
				t.Errorf("take %s: %s changed", tc.take, p)
			}
		}
		if _, err := os.Stat(filepath.Join(v, ".vault.lock.d")); err == nil {
			t.Error("the lock wasn't released")
		}
	}
	if err := os.Remove(filepath.Join(v, "missing.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(v, "missing.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, src, Options{Take: []string{"missing.md"}}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("take a directory: %v", err)
	}
}

func TestApplyRecordFormats(t *testing.T) {
	src, v := applySetup(t)
	newer := `{"format": 2, "files": {}}` + "\n"
	write(t, v, RecordName, newer)
	res, err := Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "newer ct") {
		t.Errorf("warnings %v", res.Warnings)
	}
	if read(t, v, RecordName) != newer {
		t.Error("a newer-format record was rewritten")
	}

	// An invalid record: rewritten only when files are written anyway
	src, v = applySetup(t)
	if _, err := Apply(v, src, Options{Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	write(t, v, RecordName, "{")
	res, err = Apply(v, src, Options{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || read(t, v, RecordName) != "{" {
		t.Errorf("invalid record: warnings %v, record %q", res.Warnings, read(t, v, RecordName))
	}
	if err := os.Remove(filepath.Join(v, "missing.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, src, Options{Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	loadRec(t, v)
}
