package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/session"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

// devWorkspace is a workspace PROJ-12 under a work root with two repos: a
// tix checkout (github-acme-tickets) and another one.
func devWorkspace(t *testing.T) (vault.Context, string) {
	t.Helper()
	ctx := vault.Context{}
	ctx.WorkRoot = t.TempDir()
	dir := filepath.Join(ctx.WorkRoot, "PROJ-12")
	if err := workspace.Ensure(dir, "PROJ-12", ""); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"github-acme-tickets", "github-acme-billing"} {
		path := filepath.Join(dir, slug)
		if err := os.MkdirAll(filepath.Join(path, "cmd"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := workspace.Put(workspace.File(dir), workspace.Repo{Slug: slug, Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	tickets := filepath.Join(dir, "github-acme-tickets")
	for _, d := range []string{filepath.Join("cmd", "tix"), filepath.Join("assets", "plugin")} {
		if err := os.MkdirAll(filepath.Join(tickets, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, dir
}

func TestDevSetRecordsValidatesAndClears(t *testing.T) {
	ctx, dir := devWorkspace(t)
	var quiet exitError
	if err := setDev(ctx, []string{"PROJ-12"}, "github-acme-billing"); !errors.As(err, &quiet) || quiet != 1 {
		t.Errorf("a checkout without cmd/tix: err = %v, want exit 1", err)
	}
	if err := setDev(ctx, []string{"PROJ-12"}, "github-acme-nope"); !errors.As(err, &quiet) {
		t.Errorf("a slug not in the workspace: err = %v, want exit 1", err)
	}
	if err := setDev(ctx, []string{"PROJ-99"}, "github-acme-tickets"); !errors.As(err, &quiet) {
		t.Errorf("no workspace: err = %v, want exit 1", err)
	}
	if info, _ := workspace.Read(dir); info.Dev != nil {
		t.Fatalf("a refused slug was recorded: %+v", info.Dev)
	}
	if err := setDev(ctx, []string{"PROJ-12"}, "github-acme-tickets"); err != nil {
		t.Fatal(err)
	}
	if info, _ := workspace.Read(dir); info.Dev == nil || info.Dev.Slug != "github-acme-tickets" {
		t.Fatalf("dev = %+v", info.Dev)
	}
	if err := setDev(ctx, []string{"PROJ-12"}, devNone); err != nil {
		t.Fatal(err)
	}
	if info, _ := workspace.Read(dir); info.Dev != nil {
		t.Errorf("--dev none left %+v", info.Dev)
	}
	if err := setDev(ctx, []string{"PROJ-99"}, devNone); err != nil {
		t.Errorf("--dev none without a workspace: %v", err)
	}
}

// captureStderr runs f and returns what it wrote to stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	f()
	os.Stderr = old
	w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

func TestDevSetUnknownSlugSaysAddItFirst(t *testing.T) {
	ctx, _ := devWorkspace(t)
	msg := captureStderr(t, func() { _ = setDev(ctx, []string{"PROJ-12"}, "github-acme-nope") })
	if want := "tix start: github-acme-nope is not a repo of PROJ-12's workspace (tix ws add first)\n"; msg != want {
		t.Errorf("message %q, want %q", msg, want)
	}
	// No workspace yet: the same
	msg = captureStderr(t, func() { _ = setDev(ctx, []string{"PROJ-99"}, "github-acme-tickets") })
	if !strings.Contains(msg, "github-acme-tickets is not a repo of PROJ-99's workspace (tix ws add first)") {
		t.Errorf("no workspace: message %q", msg)
	}
	// A repo that isn't a tix checkout
	msg = captureStderr(t, func() { _ = setDev(ctx, []string{"PROJ-12"}, "github-acme-billing") })
	if want := "tix start: github-acme-billing is not a tix checkout\n"; msg != want {
		t.Errorf("message %q, want %q", msg, want)
	}
}

func TestDevSetChecksEveryKeyBeforeRecording(t *testing.T) {
	ctx, dir := devWorkspace(t)
	var quiet exitError
	if err := setDev(ctx, []string{"PROJ-12", "PROJ-99"}, "github-acme-tickets"); !errors.As(err, &quiet) {
		t.Fatalf("a later key without the repo: err = %v, want exit 1", err)
	}
	if info, _ := workspace.Read(dir); info.Dev != nil {
		t.Errorf("an earlier key was recorded before a later one failed: %+v", info.Dev)
	}
}

// devPluginCheckout gives the workspace's tix checkout a plugin
// (a manifest and a hooks.json running tix from PATH).
func devPluginCheckout(t *testing.T, dir string) string {
	t.Helper()
	plugin := filepath.Join(dir, "github-acme-tickets", "assets", "plugin")
	for name, body := range map[string]string{
		filepath.Join(".claude-plugin", "plugin.json"): `{"name": "tix"}`,
		filepath.Join("hooks", "hooks.json"):           `{"command": "tix hook agent-state idle"}`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(plugin, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(plugin, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return plugin
}

func TestDevPluginCopiedWithoutRunningTheBuild(t *testing.T) {
	_, dir := devWorkspace(t)
	devPluginCheckout(t, dir)
	if err := workspace.SetDev(workspace.File(dir), &workspace.Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	old := goBuild
	t.Cleanup(func() { goBuild = old })
	builds := 0
	goBuild = func(checkout, out string, env map[string]string) ([]byte, error) {
		builds++
		// Not a program: running it would fail the start
		return nil, os.WriteFile(out, []byte("not a program"), 0o644)
	}
	b, err := prepareDev(dir, devRebuild)
	if err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Errorf("%d builds, want 1", builds)
	}
	if want := filepath.Join(session.DevDir(dir), "bin", tixFile()); b.tix != want || b.bin != filepath.Dir(want) {
		t.Errorf("build %+v, want tix %s", b, want)
	}
	data, err := os.ReadFile(filepath.Join(b.plugin, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks map[string]string
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatal(err)
	}
	if want := `"` + b.tix + `" hook agent-state idle`; hooks["command"] != want {
		t.Errorf("hook = %q, want %q", hooks["command"], want)
	}
	// tix clean ignores it
	if b, err := prepareDev(dir, devIgnore); b != nil || err != nil {
		t.Errorf("ignore: %+v, %v", b, err)
	}
}

func TestDevPluginRefusesANonPluginTarget(t *testing.T) {
	_, dir := devWorkspace(t)
	devPluginCheckout(t, dir)
	if err := workspace.SetDev(workspace.File(dir), &workspace.Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	old := goBuild
	t.Cleanup(func() { goBuild = old })
	goBuild = func(checkout, out string, env map[string]string) ([]byte, error) { return nil, nil }
	keep := filepath.Join(session.DevDir(dir), "plugin", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDev(dir, devRebuild); err == nil {
		t.Fatal("replaced a dir that isn't a plugin")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the refused dir lost a file: %v", err)
	}
}

func TestDevPrepareOffAndFailedBuild(t *testing.T) {
	_, dir := devWorkspace(t)
	if b, err := prepareDev(dir, devRebuild); b != nil || err != nil {
		t.Fatalf("not a dev workspace: %+v, %v", b, err)
	}
	if b, err := prepareDev(filepath.Join(t.TempDir(), "none"), devRebuild); b != nil || err != nil {
		t.Fatalf("no workspace: %+v, %v", b, err)
	}
	if err := workspace.SetDev(workspace.File(dir), &workspace.Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	var gotDir, gotOut string
	var gotEnv map[string]string
	old := goBuild
	t.Cleanup(func() { goBuild = old })
	goBuild = func(checkout, out string, env map[string]string) ([]byte, error) {
		gotDir, gotOut, gotEnv = checkout, out, env
		return []byte("syntax error\n"), errors.New("exit status 1")
	}
	var quiet exitError
	if b, err := prepareDev(dir, devRebuild); b != nil || !errors.As(err, &quiet) || quiet != 1 {
		t.Fatalf("a failed build: %+v, %v; want exit 1", b, err)
	}
	if gotDir != filepath.Join(dir, "github-acme-tickets") {
		t.Errorf("built in %s", gotDir)
	}
	if want := filepath.Join(session.DevDir(dir), "bin", tixFile()); gotOut != want {
		t.Errorf("built into %s, want %s", gotOut, want)
	}
	if gotEnv["GOCACHE"] != session.GoEnv(dir)["GOCACHE"] {
		t.Errorf("not the workspace's Go env: %v", gotEnv)
	}
	// A recorded checkout that's gone fails too
	if err := workspace.Drop(workspace.File(dir), "github-acme-tickets"); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDev(dir, devRebuild); !errors.As(err, &quiet) {
		t.Errorf("a recorded checkout gone: err = %v, want exit 1", err)
	}
}

func TestDevPluginRefusesALinkedParentOutsideTheCheckout(t *testing.T) {
	_, dir := devWorkspace(t)
	devPluginCheckout(t, dir)
	if err := workspace.SetDev(workspace.File(dir), &workspace.Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	old := goBuild
	t.Cleanup(func() { goBuild = old })
	goBuild = func(checkout, out string, env map[string]string) ([]byte, error) { return nil, nil }
	assetsDir := filepath.Join(dir, "github-acme-tickets", "assets")
	outside := filepath.Join(t.TempDir(), "assets")
	if err := os.Rename(assetsDir, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, assetsDir); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, err := prepareDev(dir, devRebuild); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if _, err := os.Stat(filepath.Join(session.DevDir(dir), "plugin")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plugin copied from outside the checkout (%v)", err)
	}
}

func TestDevPluginNoAssetsDirSaysSo(t *testing.T) {
	_, dir := devWorkspace(t)
	devPluginCheckout(t, dir)
	if err := workspace.SetDev(workspace.File(dir), &workspace.Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	old := goBuild
	t.Cleanup(func() { goBuild = old })
	// the checkout loses assets/ during the build (another branch)
	goBuild = func(checkout, out string, env map[string]string) ([]byte, error) {
		return nil, os.RemoveAll(filepath.Join(checkout, "assets"))
	}
	_, err := prepareDev(dir, devRebuild)
	if err == nil || err.Error() != "copying the plugin of github-acme-tickets: no assets dir in github-acme-tickets" {
		t.Fatalf("err = %v, want the missing assets dir named", err)
	}
}

func TestGoBuildNoVCS(t *testing.T) {
	cmd := goBuildCmd("/src", "/out/tix", map[string]string{"GOFLAGS": "-mod=mod"})
	if !slices.Contains(cmd.Args, "-buildvcs=false") {
		t.Fatalf("go build runs git in the checkout (no -buildvcs=false): %q", cmd.Args)
	}
	if cmd.Dir != "/src" || !slices.Contains(cmd.Env, "GOFLAGS=-mod=mod") {
		t.Fatalf("dir %q, env lacks GOFLAGS", cmd.Dir)
	}
}
