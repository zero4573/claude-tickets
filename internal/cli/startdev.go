package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// devNone is the --dev value that turns a dev workspace back into a plain one.
const devNone = "none"

const devHelp = "run the workspace's sessions on the plugin and ct built from its claude-tickets checkout <slug> (kept for later starts; none: back to the installed ones)"

// goBuild builds ct from a claude-tickets checkout into out, with env added
// to ct's own, and returns go's output. A seam: tests swap it.
var goBuild = func(checkout, out string, env map[string]string) ([]byte, error) {
	return goBuildCmd(checkout, out, env).CombinedOutput()
}

// goBuildCmd is the go build of goBuild. -buildvcs=false: stamping VCS
// info runs git in the checkout, outside ct's checked git, where the
// checkout's own config (fsmonitor, filters) could run a program.
func goBuildCmd(checkout, out string, env map[string]string) *exec.Cmd {
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", out, "./cmd/ct")
	cmd.Dir = checkout
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

// ctFile is the name of a ct executable on this OS.
func ctFile() string {
	if runtime.GOOS == "windows" {
		return "ct.exe"
	}
	return "ct"
}

// devCheckout is the checkout of the workspace repo slug when it's a
// claude-tickets one (it has cmd/ct and assets/plugin).
func devCheckout(info workspace.Info, slug string) (string, bool) {
	for _, r := range info.Repos {
		if r.Slug == slug && fsx.IsDir(filepath.Join(r.Path, "cmd", "ct")) && fsx.IsDir(filepath.Join(r.Path, "assets", "plugin")) {
			return r.Path, true
		}
	}
	return "", false
}

func notDevCheckout(slug string) error {
	fmt.Fprintf(os.Stderr, "ct start: %s is not a claude-tickets checkout\n", slug)
	return exitError(1)
}

// checkDev is nil when slug can be key's dev checkout (or is none):
// a repo of its workspace that is a claude-tickets checkout.
func checkDev(ctx vault.Context, key, slug string) error {
	if slug == devNone {
		return nil
	}
	info, err := workspace.Read(filepath.Join(ctx.WorkRoot, key))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !slices.ContainsFunc(info.Repos, func(r workspace.Repo) bool { return r.Slug == slug }) {
		fmt.Fprintf(os.Stderr, "ct start: %s is not a repo of %s's workspace (ct ws add first)\n", slug, key)
		return exitError(1)
	}
	if _, ok := devCheckout(info, slug); !ok {
		return notDevCheckout(slug)
	}
	return nil
}

// setDev records slug as the dev checkout of each of keys' workspaces, or
// with none clears it. Every key is checked (checkDev) before any is
// recorded.
func setDev(ctx vault.Context, keys []string, slug string) error {
	for _, key := range keys {
		if err := checkDev(ctx, key, slug); err != nil {
			return err
		}
	}
	for _, key := range keys {
		file := workspace.File(filepath.Join(ctx.WorkRoot, key))
		if slug == devNone {
			if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err := workspace.SetDev(file, nil); err != nil {
				return err
			}
			continue
		}
		if err := workspace.SetDev(file, &workspace.Dev{Slug: slug}); err != nil {
			return err
		}
	}
	return nil
}

// devBuild is a dev workspace's build: its plugin dir, the dir of its ct
// (first on the sessions' PATH) and that ct.
type devBuild struct {
	plugin, bin, ct string
}

// devMode is what prepareTicketSession does with a dev workspace's build.
type devMode int

const (
	devIgnore  devMode = iota // the installed plugin and ct (ct clean)
	devRebuild                // build ct and copy the plugin first
)

// prepareDev is the build of a dev workspace (nil when dir isn't one or
// mode is devIgnore). With devRebuild it builds ct into <dir>/.cache/dev/bin
// and copies the checkout's plugin to <dir>/.cache/dev/plugin, its hooks
// running that ct; it never runs the built ct. A failed build fails:
// nothing falls back to the installed ones.
func prepareDev(dir string, mode devMode) (*devBuild, error) {
	if mode == devIgnore {
		return nil, nil
	}
	info, err := workspace.Read(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Dev == nil {
		return nil, nil
	}
	b := devBuild{plugin: filepath.Join(session.DevDir(dir), "plugin"), bin: filepath.Join(session.DevDir(dir), "bin")}
	b.ct = filepath.Join(b.bin, ctFile())
	slug := info.Dev.Slug
	checkout, ok := devCheckout(info, slug)
	if !ok {
		return nil, notDevCheckout(slug)
	}
	if err := os.MkdirAll(b.bin, 0o755); err != nil {
		return nil, err
	}
	if out, err := goBuild(checkout, b.ct, session.GoEnv(dir)); err != nil {
		os.Stderr.Write(out)
		fmt.Fprintf(os.Stderr, "ct start: building ct in %s failed (%v)\n", slug, err)
		return nil, exitError(1)
	}
	// The checkout may have changed during the build (another branch).
	if _, err := os.Lstat(filepath.Join(checkout, "assets")); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("copying the plugin of %s: no assets dir in %s", slug, slug)
	}
	// Read through an os.Root at the checkout: a linked assets/ (or any
	// link swapped in during the copy) can't reach outside it.
	if err := assets.MaterializeTo(checkout, "assets/plugin", b.plugin, b.ct); err != nil {
		return nil, fmt.Errorf("copying the plugin of %s: %w", slug, err)
	}
	fmt.Printf("ct start: dev mode: plugin and ct from %s (%s)\n", slug, devVersion(checkout))
	return &b, nil
}

// devVersion is checkout's short HEAD sha, with -dirty when it has
// uncommitted changes (the build has them too).
func devVersion(checkout string) string {
	sha, err := gitx.Out(checkout, "rev-parse", "--short", "HEAD")
	sha = strings.TrimSpace(sha)
	if err != nil || sha == "" {
		return "unknown HEAD"
	}
	if st, err := gitx.Out(checkout, "status", "--porcelain"); err == nil && strings.TrimSpace(st) != "" {
		sha += "-dirty"
	}
	return sha
}
