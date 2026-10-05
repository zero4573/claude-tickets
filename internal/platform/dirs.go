// Package platform holds everything that differs between operating
// systems: where claude-tickets keeps its files, and the desktop tools it
// uses (notifications, opening files, a shell). Every OS specific belongs
// here (or behind a build tag in the package that owns it), so the rest
// of the code stays the same on Linux, macOS and Windows.
package platform

import (
	"os"
	"path"
	"runtime"
)

type Dirs struct {
	Config string // settings: config.json, default-vault
	Cache  string // rebuildable: the graph image's filesystem, unpacked assets
	Logs   string // command logs (ticket-sync-<vault>.log, ...)
}

const app = "claude-tickets"

// DirsFor, per GOOS:
//
//	linux    $XDG_CONFIG_HOME|~/.config/claude-tickets, $XDG_CACHE_HOME|~/.cache/claude-tickets,
//	         logs in $XDG_STATE_HOME|~/.local/state
//	darwin   ~/Library/Application Support/claude-tickets, ~/Library/Caches/claude-tickets,
//	         ~/Library/Logs/claude-tickets
//	windows  %AppData%\claude-tickets, %LocalAppData%\claude-tickets\cache,
//	         %LocalAppData%\claude-tickets\logs
//
// On every OS, CLAUDE_TICKETS_CONFIG_DIR, CLAUDE_TICKETS_CACHE and
// CLAUDE_TICKETS_STATE_DIR win, then the XDG variables when set.
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
	if x := getenv("CLAUDE_TICKETS_CONFIG_DIR"); x != "" {
		d.Config = x
	}
	if x := getenv("CLAUDE_TICKETS_CACHE"); x != "" {
		d.Cache = x
	}
	if x := getenv("CLAUDE_TICKETS_STATE_DIR"); x != "" {
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
