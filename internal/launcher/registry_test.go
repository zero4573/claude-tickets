package launcher

import (
	"errors"
	"slices"
	"testing"
)

type fake struct{ name string }

func (f fake) Name() string                        { return f.name }
func (fake) Caps() Caps                            { return Caps{Background: true} }
func (fake) Windows(string) []string               { return nil }
func (fake) Open(Window) error                     { return nil }
func (fake) SendKeys(string, string, string) error { return nil }
func (fake) Attach(string, string) error           { return nil }

// fakes is a registry like backends, with tmux installed or not.
func fakes(tmux bool, extra ...Backend) []Backend {
	avail := func(ok bool) func() error {
		return func() error {
			if ok {
				return nil
			}
			return errors.New("not on PATH")
		}
	}
	bs := []Backend{{Name: "tmux", New: func() Launcher { return fake{"tmux"} }, Available: avail(tmux)}}
	bs = append(bs, extra...)
	return append(bs, Backend{Name: "none", New: func() Launcher { return none{} }, Available: avail(true)})
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name, env, cfg string
		tmux           bool
		want           string
		src            Source
		err            string
	}{
		{name: "env over config", env: "none", cfg: "tmux", tmux: true, want: "none", src: FromEnv},
		{name: "config over detection", cfg: "none", tmux: true, want: "none", src: FromConfig},
		{name: "empty env is unset", env: "", cfg: "none", tmux: true, want: "none", src: FromConfig},
		{name: "blank env is unset", env: "  ", cfg: "none", tmux: true, want: "none", src: FromConfig},
		{name: "trimmed and case-insensitive", env: " TMUX ", tmux: true, want: "tmux", src: FromEnv},
		{name: "config trimmed and case-insensitive", cfg: "None ", tmux: true, want: "none", src: FromConfig},
		{name: "detected tmux", tmux: true, want: "tmux", src: FromDetected},
		{name: "detected none", tmux: false, want: "none", src: FromDetected},
		{name: "unknown from env", env: " tmx ", tmux: true,
			err: "unknown launcher 'tmx' (from CLAUDE_TICKETS_LAUNCHER): use tmux or none"},
		{name: "unknown from config", cfg: "zellij", tmux: true,
			err: `unknown launcher 'zellij' (from config.json "launcher"): use tmux or none`},
		{name: "explicit tmux missing", env: "tmux", tmux: false,
			err: `tmux isn't installed (CLAUDE_TICKETS_LAUNCHER / config.json "launcher"); install it, or set the launcher to none`},
		{name: "explicit tmux in config missing", cfg: "tmux", tmux: false,
			err: `tmux isn't installed (CLAUDE_TICKETS_LAUNCHER / config.json "launcher"); install it, or set the launcher to none`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := resolve(tc.env, tc.cfg, fakes(tc.tmux))
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Name() != tc.want || r.Source != tc.src {
				t.Errorf("got %s (%s), want %s (%s)", r.Name(), r.Source, tc.want, tc.src)
			}
		})
	}
}

// A new backend is one implementation plus one registration: resolve and
// its messages pick it up unchanged.
func TestResolveExtensible(t *testing.T) {
	zellij := Backend{Name: "zellij", New: func() Launcher { return fake{"zellij"} }, Available: func() error { return nil }}
	bs := fakes(false, zellij)
	if got := names(bs); !slices.Equal(got, []string{"tmux", "zellij", "none"}) {
		t.Errorf("names = %q", got)
	}
	r, err := resolve("zellij", "", bs)
	if err != nil || r.Name() != "zellij" {
		t.Fatalf("resolve(zellij) = %v, %v", r.Launcher, err)
	}
	// detection: the first available in registration order
	if r, _ := resolve("", "", bs); r.Name() != "zellij" || r.Source != FromDetected {
		t.Errorf("detected %s (%s), want zellij", r.Name(), r.Source)
	}
	_, err = resolve("screen", "", bs)
	if want := "unknown launcher 'screen' (from CLAUDE_TICKETS_LAUNCHER): use tmux, zellij or none"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestNames(t *testing.T) {
	if got := Names(); !slices.Equal(got, []string{"tmux", "none"}) {
		t.Errorf("Names() = %q", got)
	}
}
