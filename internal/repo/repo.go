// Package repo names repositories: their identity from the origin URL
// (provider, owner, repo), their slug, and the main clones under a
// projects root (<provider>/<owner>/<repo>).
package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func Kebab(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

type Identity struct {
	Provider, Owner, Repo string
}

// Clone is the main clone's path relative to the projects root.
func (id Identity) Clone() string { return id.Provider + "/" + id.Owner + "/" + id.Repo }

// RemoteIdentity's ok is false when url isn't a hosted remote:
//
//	bitbucket  Server/DC (/scm/<key>/<repo>, ssh :7999/<key>/<repo>; owner =
//	           project key, upper-cased) or Cloud (bitbucket.org; owner =
//	           workspace)
//	github     github.com or GitHub Enterprise (owner = user/org)
//	gitlab     gitlab hosts (owner = group, subgroups joined with -)
//	otherwise  the host name, kebab-cased (owner = the path before the repo)
func RemoteIdentity(url string) (Identity, bool) {
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	var rest, host, path string
	switch {
	case strings.Contains(url, "://"):
		rest = url[strings.Index(url, "://")+3:]
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		host, path, _ = strings.Cut(rest, "/")
		if !strings.Contains(rest, "/") {
			path = rest
		}
	case strings.Contains(url, "@") && strings.Contains(url, ":"):
		rest = url[strings.Index(url, "@")+1:]
		host, path, _ = strings.Cut(rest, ":")
		if !strings.Contains(rest, ":") {
			path = rest
		}
	default:
		return Identity{}, false
	}
	if path == rest || path == "" {
		return Identity{}, false
	}
	server := strings.HasSuffix(host, ":7999")
	host = strings.ToLower(strings.SplitN(host, ":", 2)[0])
	if strings.HasPrefix(path, "scm/") {
		path = strings.TrimPrefix(path, "scm/")
		server = true
	}
	var provider string
	switch {
	case host == "bitbucket.org":
		provider = "bitbucket"
	case strings.Contains(host, "bitbucket"):
		provider, server = "bitbucket", true
	case strings.Contains(host, "github"):
		provider = "github"
	case strings.Contains(host, "gitlab"):
		provider = "gitlab"
	default:
		provider = Kebab(host)
	}
	if server {
		provider = "bitbucket"
	}
	i := strings.LastIndex(path, "/")
	if i <= 0 || i == len(path)-1 {
		return Identity{}, false
	}
	owner := strings.ReplaceAll(path[:i], "/", "-")
	repoName := path[i+1:]
	if provider == "bitbucket" && host != "bitbucket.org" {
		owner = strings.ToUpper(owner)
	}
	return Identity{provider, owner, repoName}, true
}

// Slug names a repo wherever the vault or the graph does
// (<provider>-<owner>-<repo>, kebab-case), from <provider>/<owner>/<repo>.
func Slug(clone string) string { return Kebab(strings.ReplaceAll(clone, "/", "-")) }

func MainClones(root string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, "*", "*", "*", ".git"))
	var out []string
	for _, g := range matches {
		if st, err := os.Stat(g); err == nil && st.IsDir() {
			rel, _ := filepath.Rel(root, filepath.Dir(g))
			out = append(out, filepath.ToSlash(rel))
		}
	}
	sort.Strings(out)
	return out
}

// Resolve turns a slug or <provider>/<owner>/<repo> into the latter; ok is
// false when there's no such main clone.
func Resolve(root, spec string) (string, bool) {
	if strings.Count(spec, "/") >= 2 {
		if st, err := os.Stat(filepath.Join(root, spec, ".git")); err == nil && st.IsDir() {
			return spec, true
		}
	}
	for _, c := range MainClones(root) {
		if Slug(c) == spec {
			return c, true
		}
	}
	return "", false
}
