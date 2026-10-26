package embed

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeLibs(t *testing.T) string {
	d := t.TempDir()
	for _, n := range depLibs {
		if err := os.WriteFile(filepath.Join(d, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestRuntimeLibDir(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	os.WriteFile(filepath.Join(root, "flake.nix"), nil, 0o644)
	built := fakeLibs(t)
	calls := 0
	build := func(r string) (string, error) {
		calls++
		if rr, _ := filepath.EvalSymlinks(root); rr != r && root != r {
			t.Errorf("flake root %q", r)
		}
		return built, nil
	}
	// Already in the search path, or the variable is set: no build.
	if got, err := runtimeLibDir([]string{fakeLibs(t)}, false, build); got != "" || err != nil {
		t.Fatalf("found: %q %v", got, err)
	}
	if got, err := runtimeLibDir(nil, true, build); got != "" || err != nil || calls != 0 {
		t.Fatalf("env set: %q %v %d", got, err, calls)
	}
	// Missing: ask nix.
	if got, err := runtimeLibDir(nil, false, build); got != built || err != nil || calls != 1 {
		t.Fatalf("build: %q %v %d", got, err, calls)
	}
	// nix fails, or builds a dir without the libraries: a clear error.
	if _, err := runtimeLibDir(nil, false, func(string) (string, error) { return "", errors.New("nix not found") }); err == nil {
		t.Fatal("want error")
	}
	if _, err := runtimeLibDir(nil, false, func(string) (string, error) { return t.TempDir(), nil }); err == nil {
		t.Fatal("want error for an empty output")
	}
}

func TestNixCmd(t *testing.T) {
	git := t.TempDir()
	os.Mkdir(filepath.Join(git, ".git"), 0o755)
	cmd, tracked := nixCmd("nix", git)
	if !tracked || cmd.Dir != git || cmd.Args[len(cmd.Args)-1] != ".#"+NixLibsAttr {
		t.Errorf("git tree: %v dir=%q tracked=%v", cmd.Args, cmd.Dir, tracked)
	}
	plain := t.TempDir()
	cmd, tracked = nixCmd("nix", plain)
	if tracked || cmd.Dir != plain || cmd.Args[len(cmd.Args)-1] != "path:"+plain+"#"+NixLibsAttr {
		t.Errorf("no .git: %v dir=%q tracked=%v", cmd.Args, cmd.Dir, tracked)
	}
}

func TestNixBuildError(t *testing.T) {
	nix := t.TempDir()
	os.WriteFile(filepath.Join(nix, "nix"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", nix)
	old := nixRun
	defer func() { nixRun = old }()
	nixRun = func(*exec.Cmd) ([]byte, error) { return nil, errors.New("boom") }
	git := t.TempDir()
	os.Mkdir(filepath.Join(git, ".git"), 0o755)
	if _, err := nixBuild(git); err == nil || !strings.Contains(err.Error(), "must be `git add`ed") {
		t.Errorf("git tree error: %v", err)
	}
	if _, err := nixBuild(t.TempDir()); err == nil || strings.Contains(err.Error(), "git add") {
		t.Errorf("non-git error: %v", err)
	}
	nixRun = func(*exec.Cmd) ([]byte, error) { return []byte("/nix/store/x\nmore\n"), nil }
	if out, err := nixBuild(git); err != nil || out != "/nix/store/x" {
		t.Errorf("ok: %q %v", out, err)
	}
}
