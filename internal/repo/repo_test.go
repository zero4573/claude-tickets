package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Expected values come from the bash remote_identity this replaces.
func TestRemoteIdentity(t *testing.T) {
	cases := []struct{ url, want string }{
		{"git@bitbucket.org:acme/billing-service.git", "bitbucket/acme/billing-service"},
		{"https://user@bitbucket.org/acme/billing-service.git", "bitbucket/acme/billing-service"},
		{"ssh://git@bitbucket.corp.example:7999/proj/repo.git", "bitbucket/PROJ/repo"},
		{"https://bitbucket.corp.example/scm/proj/repo.git", "bitbucket/PROJ/repo"},
		{"git@github.com:me/foo.git", "github/me/foo"},
		{"https://github.com/me/foo", "github/me/foo"},
		{"https://github.example.com/org/foo.git", "github/org/foo"},
		{"git@gitlab.com:group/sub/proj.git", "gitlab/group-sub/proj"},
		{"https://git.example.org/team/x.git", "git-example-org/team/x"},
		{"https://github.com/me/foo/", "github/me/foo"},
		{"/local/path/repo", ""},
		{"noslash", ""},
		{"git@github.com:foo.git", ""},
		{"git@bitbucket.org:keep/..", ""},
		{"git@bitbucket.org:../repo.git", ""},
		{"git@bitbucket.org:a/./repo.git", ""},
		{"git@bitbucket.org:-x/repo.git", ""},
		{"git@bitbucket.org:a/-repo.git", ""},
		{`git@bitbucket.org:x\..\..\..\..\a\b.git`, ""},
		{`git@bitbucket.org:x/..\..\b.git`, ""},
		{"git@bitbucket.org:a/C:repo.git", ""},
		{"https://github.com/C:/x.git", ""},
	}
	for _, c := range cases {
		got := ""
		if id, ok := RemoteIdentity(c.url); ok {
			got = id.Clone()
		}
		if got != c.want {
			t.Errorf("RemoteIdentity(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("bitbucket/PROJ/My_Repo"); got != "bitbucket-proj-my-repo" {
		t.Errorf("Slug = %q", got)
	}
}

func TestNarrow(t *testing.T) {
	cases := []struct {
		name string
		dirs []string
		want string
	}{
		{"empty", nil, ""},
		{"two providers", []string{"github/acme/x", "bitbucket/acme/y"}, ""},
		{"one provider, two owners", []string{"github/acme/x", "github/other/y"}, "github"},
		{"one owner, two repos", []string{"github/acme/x", "github/acme/y"}, "github/acme"},
		{"one repo: not into it", []string{"github/acme/x"}, "github/acme"},
		{"dot-folders and files aside", []string{".cache/a", "github/acme/x", "github/.tmp"}, "github/acme"},
	}
	for _, c := range cases {
		root := t.TempDir()
		for _, d := range c.dirs {
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, filepath.FromSlash(c.want))
		if got := Narrow(root); got != want {
			t.Errorf("%s: Narrow = %s, want %s", c.name, got, want)
		}
	}
	if missing := filepath.Join(t.TempDir(), "x"); Narrow(missing) != missing {
		t.Errorf("missing root: Narrow = %s", Narrow(missing))
	}
}

func TestCloneDest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "p")
	if d, err := CloneDest(root, Identity{"github", "acme", "billing"}); err != nil || d != filepath.Join(root, "github", "acme", "billing") {
		t.Errorf("CloneDest = %q, %v", d, err)
	}
	// identities RemoteIdentity never makes, to reach the check itself
	for _, id := range []Identity{
		{"x", "..", ".."},
		{"x", "../../..", "b"},
		{"x", "y", "../../../../a/b"},
		{"x", "y", "z/w"},
		{"x", ".", "z"},
	} {
		if d, err := CloneDest(root, id); err == nil {
			t.Errorf("CloneDest(%v) = %q, want an error", id, d)
		}
	}
}

func TestCheckCloneParent(t *testing.T) {
	dir := t.TempDir()
	root, out := filepath.Join(dir, "p"), filepath.Join(dir, "out")
	for _, d := range []string{filepath.Join(root, "github", "acme"), filepath.Join(root, "github", "inner"), out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckCloneParent(root, filepath.Join(root, "github", "acme", "billing")); err != nil {
		t.Errorf("a plain owner folder: %v", err)
	}
	if err := os.Symlink(out, filepath.Join(root, "github", "evil")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := CheckCloneParent(root, filepath.Join(root, "github", "evil", "billing")); err == nil {
		t.Error("an owner folder symlinked out of the root: no error")
	}
	if err := os.Symlink(filepath.Join(root, "github"), filepath.Join(root, "github", "up")); err != nil {
		t.Fatal(err)
	}
	if err := CheckCloneParent(root, filepath.Join(root, "github", "up", "billing")); err == nil {
		t.Error("an owner folder symlinked to the provider level: no error")
	}
	// the root itself behind a symlink is fine
	link := filepath.Join(dir, "plink")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckCloneParent(link, filepath.Join(link, "github", "acme", "billing")); err != nil {
		t.Errorf("a symlinked projects root: %v", err)
	}
}

func TestCheckCloneAncestors(t *testing.T) {
	dir := t.TempDir()
	root, out := filepath.Join(dir, "p"), filepath.Join(dir, "out")
	for _, d := range []string{filepath.Join(root, "github"), out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckCloneAncestors(root, filepath.Join(root, "github", "new", "billing")); err != nil {
		t.Errorf("missing owner under a real provider: %v", err)
	}
	if err := CheckCloneAncestors(root, filepath.Join(root, "gitlab", "new", "billing")); err != nil {
		t.Errorf("missing provider under the root: %v", err)
	}
	if err := os.Symlink(out, filepath.Join(root, "evil")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := CheckCloneAncestors(root, filepath.Join(root, "evil", "acme", "billing")); err == nil {
		t.Error("a provider folder symlinked out of the root: no error")
	}
}
