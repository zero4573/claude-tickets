package embed

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zero4573/tix/internal/gitx"
)

// NixLibsAttr is the flake output that holds libgomp and libstdc++.
const NixLibsAttr = "llamaRuntimeLibs"

// RuntimeLibDir is for tests and benchmarks (the shipped binary does not call
// it, its search order is unchanged). It returns a directory to put in
// TIX_LIB_PATH so the llama.cpp runtime libraries are found without
// setting it by hand, or "" when nothing needs adding: the variable is set, the
// libraries are already in the search path, or there is no nix (the system
// loader is then trusted). Otherwise it builds .#llamaRuntimeLibs from the
// flake that contains the working directory.
func RuntimeLibDir() (string, error) {
	return runtimeLibDir(libSearchDirs(), os.Getenv("TIX_LIB_PATH") != "", nixBuild)
}

func runtimeLibDir(dirs []string, envSet bool, build func(flakeRoot string) (string, error)) (string, error) {
	if envSet {
		return "", nil
	}
	missing := false
	for _, n := range depLibs {
		if findLib(dirs, n) == "" {
			missing = true
		}
	}
	if !missing {
		return "", nil
	}
	root, err := flakeRoot()
	if err != nil {
		return "", err
	}
	out, err := build(root)
	if err != nil {
		return "", fmt.Errorf("finding %s: %w (set TIX_LIB_PATH to a directory holding them)", strings.Join(depLibs, ", "), err)
	}
	for _, n := range depLibs {
		if findLib([]string{out}, n) == "" {
			return "", fmt.Errorf("%s has no %s (set TIX_LIB_PATH)", out, n)
		}
	}
	return out, nil
}

// flakeRoot walks up from the working directory to the directory with flake.nix.
func flakeRoot() (string, error) {
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "flake.nix")); err == nil {
			return d, nil
		}
		p := filepath.Dir(d)
		if p == d {
			return "", fmt.Errorf("no flake.nix above the working directory")
		}
		d = p
	}
}

// nixCmd builds the nix build command for root: a git tree is built as the
// git flake (cmd.Dir = root, tracked files only); a directory without .git
// has no git flake, so it is addressed as path:<root>#attr.
func nixCmd(nix, root string) (cmd *exec.Cmd, gitTree bool) {
	_, err := os.Stat(filepath.Join(root, ".git"))
	gitTree = err == nil
	ref := ".#" + NixLibsAttr
	if !gitTree {
		ref = "path:" + root + "#" + NixLibsAttr
	}
	cmd = exec.Command(nix, "--extra-experimental-features", "nix-command flakes",
		"build", "--no-link", "--print-out-paths", ref)
	cmd.Dir = root
	return cmd, gitTree
}

// nixRun runs the build command (a seam for tests).
var nixRun = func(cmd *exec.Cmd) ([]byte, error) {
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func nixBuild(root string) (string, error) {
	nix, err := exec.LookPath("nix")
	if err != nil {
		return "", fmt.Errorf("nix not found")
	}
	cmd, gitTree := nixCmd(nix, root)
	if gitTree {
		// nix reads the git tree through git: the repo's own config must
		// pass tix's allowlist first, as for any git tix runs
		if err := gitx.Check(root); err != nil {
			return "", err
		}
	}
	b, err := nixRun(cmd)
	if err != nil {
		if gitTree {
			return "", fmt.Errorf("nix build .#%s: %w (new untracked files must be `git add`ed: the flake builds tracked files only)", NixLibsAttr, err)
		}
		return "", fmt.Errorf("nix build path:%s#%s: %w", root, NixLibsAttr, err)
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0]), nil
}
