package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/zero4573/claude-tickets/internal/config"
)

// EnvVar and ConfigKey (config.json) pick the backend.
const (
	EnvVar    = "CLAUDE_TICKETS_LAUNCHER"
	ConfigKey = "launcher"
)

// Backend is a registered launcher: its name (the setting's value), how to
// make one, and whether it's usable here.
type Backend struct {
	Name      string
	New       func() Launcher
	Available func() error // nil when usable here (e.g. its binary is on PATH)
}

// backends in auto-detection order; none is last and always available.
// A new backend is one Launcher implementation plus one entry here.
var backends = []Backend{
	{Name: "tmux", New: func() Launcher { return Tmux{} }, Available: onPath("tmux")},
	{Name: "none", New: func() Launcher { return none{} }, Available: func() error { return nil }},
}

func onPath(bin string) func() error {
	return func() error {
		_, err := exec.LookPath(bin)
		return err
	}
}

// Source is where the launcher in effect was chosen.
type Source string

const (
	FromEnv      Source = EnvVar
	FromConfig   Source = "config.json"
	FromDetected Source = "detected"
)

type Resolved struct {
	Launcher
	Source Source
}

// Names is the registered backends' names, in detection order.
func Names() []string { return names(backends) }

func names(bs []Backend) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
	}
	return out
}

// Resolve picks the launcher: CLAUDE_TICKETS_LAUNCHER (an empty one is
// unset), else config.json's launcher, else the first registered backend
// that's available. A value is trimmed and case-insensitive; an unknown
// one, or an explicit backend that isn't installed, is an error (no
// silent fallback).
func Resolve() (Resolved, error) {
	return resolve(os.Getenv(EnvVar), config.Get(ConfigKey), backends)
}

func resolve(env, cfg string, bs []Backend) (Resolved, error) {
	v, src := strings.TrimSpace(env), FromEnv
	if v == "" {
		v, src = strings.TrimSpace(cfg), FromConfig
	}
	if v == "" {
		for _, b := range bs {
			if b.Available() == nil {
				return Resolved{b.New(), FromDetected}, nil
			}
		}
		return Resolved{}, fmt.Errorf("no launcher available: install %s", orList(names(bs)))
	}
	from := EnvVar
	if src == FromConfig {
		from = `config.json "` + ConfigKey + `"`
	}
	for _, b := range bs {
		if strings.EqualFold(v, b.Name) {
			if b.Available() != nil {
				return Resolved{}, fmt.Errorf(`%s isn't installed (%s / config.json "%s"); install it, or set the launcher to none`, b.Name, EnvVar, ConfigKey)
			}
			return Resolved{b.New(), src}, nil
		}
	}
	return Resolved{}, fmt.Errorf("unknown launcher '%s' (from %s): use %s", v, from, orList(names(bs)))
}

// orList is "a", "a or b", "a, b or c".
func orList(s []string) string {
	if len(s) <= 1 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}
