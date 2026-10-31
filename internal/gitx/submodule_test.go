package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A nested repository committed as a gitlink (its own .git folder in the
// work tree, which Check doesn't read) with a planted clean filter, and a
// .gitmodules asking for its dirtiness: tix's status and diffs don't run
// git in it.
func TestInTreeGitlinkNotEntered(t *testing.T) {
	sup, marker, _ := plantedRepo(t)
	inner := filepath.Join(sup, "inner")
	git(t, sup, "init", "-q", inner)
	write(t, filepath.Join(inner, "f"), "a\n")
	write(t, filepath.Join(inner, ".gitattributes"), "f filter=evil\n")
	git(t, inner, "add", ".")
	git(t, inner, "commit", "-qm", "x")
	filt := filepath.Join(filepath.Dir(sup), "filt")
	write(t, filt, "#!/bin/sh\ntouch "+marker+"\ncat\n")
	os.Chmod(filt, 0o755)
	git(t, inner, "config", "filter.evil.clean", filt)
	write(t, filepath.Join(sup, ".gitmodules"), "[submodule \"inner\"]\n\tpath = inner\n\turl = https://example.invalid/inner.git\n\tignore = none\n")
	git(t, sup, "add", ".gitmodules", "inner")
	git(t, sup, "commit", "-qm", "gitlink")
	write(t, filepath.Join(inner, "f"), "b\n")
	if err := Check(sup); err != nil {
		t.Fatalf("Check reads the nested repo's config now (%v); this test no longer proves the flag", err)
	}
	plain := exec.Command("git", "-C", sup, "-c", "diff.ignoreSubmodules=all", "status", "--porcelain")
	plain.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
	_ = plain.Run()
	if !ran(marker) {
		t.Skip("plain git doesn't enter the nested repo here; nothing to prove")
	}
	os.Remove(marker)
	Dirty(sup)
	for _, args := range [][]string{{"diff"}, {"diff-files"}, {"diff-index", "HEAD"}, {"status", "--porcelain=2"}} {
		_, _ = Out(sup, args...)
	}
	if ran(marker) {
		t.Error("tix's git ran the nested repo's clean filter")
	}
	for _, args := range [][]string{{"add", "-A"}, {"add", "--update"}, {"add", "-qu", "x"}, {"commit", "-am", "x"},
		{"commit", "--all", "-m", "x"}, {"status", "--ignore-submodules=none"}, {"diff", "--ignore-submodules=dirty"},
		{"add", "."}, {"add", "--", "inner"}, {"describe", "--dirty"}, {"describe", "--broken=-b"}, {"describe", "--dir"},
		{"status", "--ignore-submodules=all", "--ignore-sub=none"}, {"diff", "--ignore-s=none"}} {
		if _, err := Out(sup, args...); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("git %v: %v, want a refusal", args, err)
		}
	}
	if _, err := noSubmoduleChildren([]string{"worktree", "remove", "wt"}); err == nil {
		t.Error("worktree remove without --force: want a refusal")
	}
	if _, err := noSubmoduleChildren([]string{"worktree", "remove", "--force", "wt"}); err != nil {
		t.Errorf("worktree remove --force: %v", err)
	}
	if _, err := noSubmoduleChildren([]string{"commit", "-ma", "--", "-a"}); err != nil {
		t.Errorf("a cluster's value and a path after --: %v", err)
	}
	for in, want := range map[string]string{
		"-C x diff --quiet -- p":                      "-C x diff --quiet --ignore-submodules=all -- p",
		"status --porcelain --ignore-submodules":      "status --porcelain --ignore-submodules --ignore-submodules=all",
		"diff --ignore-space-change HEAD":             "diff --ignore-space-change HEAD --ignore-submodules=all",
		"describe --tags":                             "describe --tags",
		"diff-index HEAD -- --ignore-submodules=none": "diff-index HEAD --ignore-submodules=all -- --ignore-submodules=none",
	} {
		if got, err := noSubmoduleChildren(strings.Fields(in)); err != nil || strings.Join(got, " ") != want {
			t.Errorf("%s: args %q, %v; want %s", in, got, err, want)
		}
	}
	// the flag goes last, so an earlier value (here one git would take
	// from an abbreviation tix missed) can't win: no child runs
	os.Remove(marker)
	_, _ = Out(sup, "status", "--porcelain")
	if ran(marker) {
		t.Error("tix's status ran the nested repo's clean filter")
	}
}

// tix's git runs in a submodule's own work tree: the core.worktree git
// wrote into its config under the superproject's .git/modules passes when
// it leads into the superproject's work tree.
func TestRunInSubmoduleWorkTree(t *testing.T) {
	sup, _, _ := plantedRepo(t)
	lib := filepath.Join(filepath.Dir(sup), "lib")
	git(t, filepath.Dir(sup), "init", "-q", lib)
	git(t, lib, "commit", "-q", "--allow-empty", "-m", "x")
	git(t, sup, "submodule", "add", "-q", remoteURL(t, filepath.Dir(sup), "lib"), "lib")
	git(t, sup, "commit", "-qm", "sub")
	wt := filepath.Join(sup, "lib")
	if err := CommandIn(wt, "status", "--porcelain").Run(); err != nil {
		t.Errorf("git in a submodule's work tree: %v", err)
	}
	conf := filepath.Join(sup, ".git", "modules", "lib", "config")
	git(t, wt, "config", "core.worktree", "../../../../elsewhere")
	if err := Check(wt); err == nil || !strings.Contains(err.Error(), `"core.worktree" at `+resolve(conf)) {
		t.Errorf("a submodule's core.worktree out of the superproject: %v", err)
	}
}
