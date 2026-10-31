// Package platform holds everything that differs between operating
// systems: where tix keeps its files, and the desktop tools it
// uses (notifications, opening files, a shell). Every OS specific belongs
// here (or behind a build tag in the package that owns it), so the rest
// of the code stays the same on Linux, macOS and Windows.
package platform

import (
	"os"
	"path"
	"runtime"
	"strings"
)

type Dirs struct {
	Config string // settings: config.json, default-vault
	Cache  string // rebuildable: unpacked assets, the embedding model
	Logs   string // command logs (ticket-sync-<vault>.log, ...)
}

const app = "tix"

// DirsFor, per GOOS:
//
//	linux    $XDG_CONFIG_HOME|~/.config/tix, $XDG_CACHE_HOME|~/.cache/tix,
//	         logs in $XDG_STATE_HOME|~/.local/state
//	darwin   ~/Library/Application Support/tix, ~/Library/Caches/tix,
//	         ~/Library/Logs/tix
//	windows  %AppData%\tix, %LocalAppData%\tix\cache,
//	         %LocalAppData%\tix\logs
//
// On every OS, TIX_CONFIG_DIR, TIX_CACHE and
// TIX_STATE_DIR win, then the XDG variables when set.
func DirsFor(goos string, getenv func(string) string, home string) Dirs {
	// Unix paths join with / whatever OS computes them (tested everywhere)
	join := path.Join
	if goos == "windows" {
		join = windowsJoin
	}
	var d Dirs
	switch goos {
	case "darwin":
		lib := join(home, "Library")
		d = Dirs{join(lib, "Application Support", app), join(lib, "Caches", app), join(lib, "Logs", app)}
	case "windows":
		roaming, local := getenv("AppData"), getenv("LocalAppData")
		if roaming == "" {
			roaming = join(home, "AppData", "Roaming")
		}
		if local == "" {
			local = join(home, "AppData", "Local")
		}
		d = Dirs{join(roaming, app), join(local, app, "cache"), join(local, app, "logs")}
	default:
		d = Dirs{join(home, ".config", app), join(home, ".cache", app), join(home, ".local", "state")}
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		d.Config = join(x, app)
	}
	if x := getenv("XDG_CACHE_HOME"); x != "" {
		d.Cache = join(x, app)
	}
	if x := getenv("XDG_STATE_HOME"); x != "" {
		d.Logs = x
	}
	if x := getenv("TIX_CONFIG_DIR"); x != "" {
		d.Config = x
	}
	if x := getenv("TIX_CACHE"); x != "" {
		d.Cache = x
	}
	if x := getenv("TIX_STATE_DIR"); x != "" {
		d.Logs = x
	}
	return d
}

// windowsJoin joins with backslashes whatever OS runs it (DirsFor is
// tested for Windows on Linux too).
func windowsJoin(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" && out[len(out)-1] != '\\' {
			out += `\`
		}
		out += p
	}
	return out
}

// Home is the user's home directory: $HOME, else the OS's idea of it
// (%USERPROFILE% on Windows).
func Home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func Current() Dirs { return DirsFor(runtime.GOOS, os.Getenv, Home()) }

// legacyApp is the name tix had before (claude-tickets): its folders move
// to tix's on the first run (MigrateLegacy).
const legacyApp = "claude-tickets"

// LegacyDirsFor is DirsFor under the old name, without the TIX_*
// overrides: where an older install kept its files.
func LegacyDirsFor(goos string, getenv func(string) string, home string) Dirs {
	noOverrides := func(k string) string {
		switch k {
		case "TIX_CONFIG_DIR", "TIX_CACHE", "TIX_STATE_DIR":
			return ""
		}
		return getenv(k)
	}
	d := DirsFor(goos, noOverrides, home)
	swap := func(p string) string {
		// the app name is the last element, or (Windows cache and logs) the
		// one before it
		sep := "/"
		if goos == "windows" {
			sep = `\`
		}
		parts := splitOn(p, sep)
		for i := len(parts) - 1; i >= 0 && i >= len(parts)-2; i-- {
			if parts[i] == app {
				parts[i] = legacyApp
				return joinOn(parts, sep)
			}
		}
		return ""
	}
	return Dirs{swap(d.Config), swap(d.Cache), swap(d.Logs)}
}

// Migration is one folder MigrateLegacy moved (or failed to).
type Migration struct {
	From, To string
	Err      error
}

// MigrateLegacy moves the folders of an install under the old name to
// tix's (config, cache, and logs where they are tix's own): each one only
// when tix's doesn't exist yet and no TIX_* variable points it elsewhere.
// A move that fails (another filesystem) is reported, the old folder left.
func MigrateLegacy(goos string, getenv func(string) string, home string) []Migration {
	cur, old := DirsFor(goos, getenv, home), LegacyDirsFor(goos, getenv, home)
	pairs := []struct {
		env      string
		from, to string
	}{
		{"TIX_CONFIG_DIR", old.Config, cur.Config},
		{"TIX_CACHE", old.Cache, cur.Cache},
		{"TIX_STATE_DIR", old.Logs, cur.Logs},
	}
	var out []Migration
	for _, p := range pairs {
		if getenv(p.env) != "" || p.from == "" || p.from == p.to {
			continue
		}
		if st, err := os.Stat(p.from); err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Lstat(p.to); err == nil {
			continue
		}
		err := os.MkdirAll(dirOf(p.to, goos), 0o755)
		if err == nil {
			err = os.Rename(p.from, p.to)
		}
		out = append(out, Migration{p.from, p.to, err})
	}
	// Windows' cache and logs were both under %LocalAppData%\claude-tickets
	if goos == "windows" {
		_ = os.Remove(dirOf(old.Cache, goos))
	}
	return out
}

func splitOn(p, sep string) []string { return strings.Split(p, sep) }

func joinOn(parts []string, sep string) string { return strings.Join(parts, sep) }

func dirOf(p, goos string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	parts := splitOn(p, sep)
	if len(parts) <= 1 {
		return p
	}
	return joinOn(parts[:len(parts)-1], sep)
}
