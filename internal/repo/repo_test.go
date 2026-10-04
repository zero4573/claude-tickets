package repo

import "testing"

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
