// Package tasksplugin installs the Obsidian Tasks community plugin into a
// vault (ct vault init): the release pinned in
// assets/obsidian/tasks-plugin.json (shared with nix/obsidian.nix),
// downloaded and checked against the pinned sha256s before anything is
// written, and swapped into place whole. An installed plugin is never
// replaced, whatever its version: Obsidian updates it.
package tasksplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/version"
)

const (
	pinFile      = "obsidian/tasks-plugin.json"
	settingsFile = "obsidian/tasks-settings.json"
	// maxFileSize caps each download (the 8.4.0 main.js is about 1 MB)
	maxFileSize = 32 << 20
	// tempPrefix starts the names of ct's temporary directories in
	// .obsidian/ (outside plugins/, so Obsidian never takes one for a plugin)
	tempPrefix = ".ct-tasks-"
	// DefaultTimeout bounds the download of all the files together
	DefaultTimeout = 60 * time.Second
)

// Pin is a release of the plugin: its id (the folder under
// .obsidian/plugins), the GitHub repo it's released from, the version, and
// the hex sha256 of each release asset to install.
type Pin struct {
	ID      string            `json:"id"`
	Repo    string            `json:"repo"`
	Version string            `json:"version"`
	SHA256  map[string]string `json:"sha256"`
}

// ParsePin reads a pin file and checks it: id, repo and a version that
// parses, main.js and manifest.json among the files, and every hash 64 hex
// characters (stored lower-case).
func ParsePin(data []byte) (Pin, error) {
	var p Pin
	if err := json.Unmarshal(data, &p); err != nil {
		return Pin{}, fmt.Errorf("tasks plugin pin: %w", err)
	}
	if p.ID == "" || p.Repo == "" || strings.ContainsAny(p.ID, `/\`) {
		return Pin{}, errors.New("tasks plugin pin: id and repo must be set (id without slashes)")
	}
	if _, ok := parseVersion(p.Version); !ok {
		return Pin{}, fmt.Errorf("tasks plugin pin: version %q isn't MAJOR.MINOR.PATCH", p.Version)
	}
	for _, f := range []string{"main.js", "manifest.json"} {
		if _, ok := p.SHA256[f]; !ok {
			return Pin{}, fmt.Errorf("tasks plugin pin: no sha256 for %s", f)
		}
	}
	for f, h := range p.SHA256 {
		if f == "" || f != filepath.Base(f) || strings.ContainsAny(f, `/\`) {
			return Pin{}, fmt.Errorf("tasks plugin pin: %q isn't a file name", f)
		}
		h = strings.ToLower(h)
		if b, err := hex.DecodeString(h); err != nil || len(b) != sha256.Size {
			return Pin{}, fmt.Errorf("tasks plugin pin: the sha256 of %s isn't 64 hex characters", f)
		}
		p.SHA256[f] = h
	}
	return p, nil
}

// Embedded is the pin built into ct.
func Embedded() (Pin, error) {
	data, err := assets.Obsidian.ReadFile(pinFile)
	if err != nil {
		return Pin{}, err
	}
	return ParsePin(data)
}

// Settings is the workflow's settings for the plugin (its data.json),
// byte for byte what the Nix module installs.
func Settings() []byte {
	data, err := assets.Obsidian.ReadFile(settingsFile)
	if err != nil {
		panic(err) // embedded at build time
	}
	return data
}

// Files is the release assets to install, sorted.
func (p Pin) Files() []string {
	var files []string
	for f := range p.SHA256 {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// Status is what's in a plugin folder.
type Status int

const (
	Missing   Status = iota // no folder
	Partial                 // a folder, but not both main.js and manifest.json
	Installed               // main.js and manifest.json (what Obsidian needs to load it)
)

// Inspect looks at a plugin folder. For an installed plugin it also reads
// the version in manifest.json; err then says what's wrong with it
// (unreadable, not JSON, no version or one that doesn't parse). With any
// other status, err means the folder couldn't be looked at.
func Inspect(pluginDir string) (Status, string, error) {
	st, err := os.Stat(pluginDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Missing, "", nil
	case err != nil:
		return Missing, "", err
	case !st.IsDir():
		return Missing, "", fmt.Errorf("%s isn't a directory", pluginDir)
	}
	if !regular(filepath.Join(pluginDir, "main.js")) || !regular(filepath.Join(pluginDir, "manifest.json")) {
		return Partial, "", nil
	}
	data, err := os.ReadFile(filepath.Join(pluginDir, "manifest.json"))
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return Installed, "", fmt.Errorf("can't be read: %w", err)
	}
	var m struct {
		Version any `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Installed, "", fmt.Errorf("isn't valid JSON: %w", err)
	}
	v, _ := m.Version.(string)
	if m.Version == nil {
		return Installed, "", errors.New("has no version")
	}
	if _, ok := parseVersion(v); !ok {
		return Installed, v, fmt.Errorf("has a version that isn't MAJOR.MINOR.PATCH (%v)", m.Version)
	}
	return Installed, v, nil
}

func regular(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// Outcome is what Ensure did.
type Outcome int

const (
	NotInstalled Outcome = iota // missing, and the download or the install failed (err says why)
	Fresh                       // installed the pinned release now
	Current                     // the pinned version was already installed
	KeptNewer                   // a newer version is installed, left alone
	KeptOlder                   // an older version is installed, left alone (Obsidian updates it)
	KeptUnknown                 // installed, but its version can't be read (err says why); left alone
)

// Installer downloads the pinned release.
type Installer struct {
	Pin Pin
	// BaseURL holds <version>/<file> (a mirror); "" is the GitHub releases
	// of Pin.Repo
	BaseURL string
	Client  *http.Client  // nil: a default client (proxy settings from the environment)
	Timeout time.Duration // for all the files together; 0: DefaultTimeout
}

// URL is where a release file is downloaded from.
func (in Installer) URL(file string) string {
	base := in.BaseURL
	if base == "" {
		base = "https://github.com/" + in.Pin.Repo + "/releases/download"
	}
	return strings.TrimRight(base, "/") + "/" + in.Pin.Version + "/" + file
}

// Ensure installs the plugin into <obsDir>/plugins/<id> unless it's
// installed already, and returns what it did and the version installed
// (now or before). An installed plugin is never downloaded, written or
// replaced. Otherwise every file is downloaded and checked first; then a
// complete copy of the folder (keeping whatever a partial install had,
// such as data.json) is staged in obsDir and renamed into place, so a
// failure leaves the folder as it was. Leftovers of an interrupted run are
// removed first.
func (in Installer) Ensure(obsDir string) (Outcome, string, error) {
	removeLeftovers(obsDir)
	pluginDir := filepath.Join(obsDir, "plugins", in.Pin.ID)
	st, ver, err := Inspect(pluginDir)
	if st == Installed {
		if err != nil {
			return KeptUnknown, ver, err
		}
		c, ok := CompareVersions(ver, in.Pin.Version)
		switch {
		case !ok:
			return KeptUnknown, ver, fmt.Errorf("has a version that isn't MAJOR.MINOR.PATCH (%s)", ver)
		case c == 0:
			return Current, ver, nil
		case c > 0:
			return KeptNewer, ver, nil
		default:
			return KeptOlder, ver, nil
		}
	}
	if err != nil {
		return NotInstalled, "", err
	}
	files, err := in.download()
	if err != nil {
		return NotInstalled, "", err
	}
	if err := install(obsDir, pluginDir, st == Partial, files); err != nil {
		return NotInstalled, "", err
	}
	return Fresh, in.Pin.Version, nil
}

// download fetches every file of the pin and checks its sha256. Errors
// name the file.
func (in Installer) download() (map[string][]byte, error) {
	timeout := in.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	client := in.Client
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	files := map[string][]byte{}
	for _, f := range in.Pin.Files() {
		data, err := fetch(ctx, client, in.URL(f))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != in.Pin.SHA256[f] {
			return nil, fmt.Errorf("%s: sha256 mismatch (got %s, want %s)", f, got, in.Pin.SHA256[f])
		}
		files[f] = data
	}
	return files, nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ct/"+version.Version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s (%s)", resp.Status, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("larger than %d MiB (%s)", maxFileSize>>20, url)
	}
	return data, nil
}

// install stages the plugin folder in obsDir and renames it into place.
func install(obsDir, pluginDir string, exists bool, files map[string][]byte) (err error) {
	staging, err := os.MkdirTemp(obsDir, tempPrefix+"staging-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = fsx.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}
	if exists {
		if err := fsx.CopyTree(pluginDir, staging); err != nil {
			return err
		}
	}
	for name, data := range files {
		if err := writeFile(filepath.Join(staging, name), data); err != nil {
			return err
		}
	}
	if !exists {
		if err := os.MkdirAll(filepath.Dir(pluginDir), 0o755); err != nil {
			return err
		}
		return os.Rename(staging, pluginDir)
	}
	old := filepath.Join(obsDir, tempPrefix+"old-"+strings.TrimPrefix(filepath.Base(staging), tempPrefix+"staging-"))
	if err := os.Rename(pluginDir, old); err != nil {
		return err
	}
	if err := os.Rename(staging, pluginDir); err != nil {
		if back := os.Rename(old, pluginDir); back != nil {
			return fmt.Errorf("%w (and moving the old folder back failed: %v; it's at %s)", err, back, old)
		}
		return err
	}
	_ = fsx.RemoveAll(old)
	return nil
}

// writeFile replaces a file (a regular file, or a link a partial install
// had: the link itself, never its target). A variable, so tests can make
// a write fail.
var writeFile = func(p string, data []byte) error {
	if st, err := os.Lstat(p); err == nil && !st.Mode().IsRegular() {
		if err := fsx.RemoveAll(p); err != nil {
			return err
		}
	}
	return os.WriteFile(p, data, 0o644)
}

// removeLeftovers removes the temporary folders of a run that was
// interrupted.
func removeLeftovers(obsDir string) {
	entries, _ := os.ReadDir(obsDir)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), tempPrefix) {
			_ = fsx.RemoveAll(filepath.Join(obsDir, e.Name()))
		}
	}
}

// EnsureSettings writes settings to the plugin's data.json when it has
// none (a missing, empty or blank file); settings already there are kept.
func EnsureSettings(pluginDir string, settings []byte) (bool, error) {
	file := filepath.Join(pluginDir, "data.json")
	data, err := os.ReadFile(file)
	if err == nil && strings.TrimSpace(string(data)) != "" {
		return false, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.WriteFile(file+".tmp", settings, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(file+".tmp", file); err != nil {
		_ = os.Remove(file + ".tmp")
		return false, err
	}
	return true, nil
}

// ErrNotArray is Enable's error for a community-plugins.json that isn't a
// JSON array (it's left as it is).
var ErrNotArray = errors.New("isn't a JSON array")

// Enable lists id in <obsDir>/community-plugins.json (a missing, empty or
// blank file is an empty list), keeping the other entries in their order.
// The file is only written when id is added.
func Enable(obsDir, id string) (bool, error) {
	file := filepath.Join(obsDir, "community-plugins.json")
	list := []any{}
	data, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, err
	case strings.TrimSpace(string(data)) != "":
		var v any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if dec.Decode(&v) != nil || dec.More() {
			return false, ErrNotArray
		}
		var ok bool
		if list, ok = v.([]any); !ok {
			return false, ErrNotArray
		}
	}
	for _, x := range list {
		if s, _ := x.(string); s == id {
			return false, nil
		}
	}
	if err := config.WriteJSON(file, append(list, id)); err != nil {
		return false, err
	}
	return true, nil
}

type semver struct {
	nums [3]int
	pre  string
}

// parseVersion reads [v]MAJOR.MINOR.PATCH[-pre][+build].
func parseVersion(s string) (semver, bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v semver
	core := s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		core, v.pre = s[:i], s[i+1:]
		if v.pre == "" {
			return semver{}, false
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return semver{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return semver{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// CompareVersions compares two [v]MAJOR.MINOR.PATCH[-pre][+build]
// versions: -1, 0 or 1 as a is older than, the same as or newer than b. A
// prerelease is older than its release; two prereleases compare as
// strings. ok is false when either doesn't parse.
func CompareVersions(a, b string) (int, bool) {
	va, ok1 := parseVersion(a)
	vb, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := range va.nums {
		if va.nums[i] != vb.nums[i] {
			if va.nums[i] < vb.nums[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case va.pre == vb.pre:
		return 0, true
	case va.pre == "":
		return 1, true
	case vb.pre == "":
		return -1, true
	}
	return strings.Compare(va.pre, vb.pre), true
}
