// Package repo names repositories: their identity from the origin URL
// (provider, owner, repo), their slug, and the main clones under a
// projects root (<provider>/<owner>/<repo>).
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/tix/internal/fsx"
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
	repoName := path[i+1:]
	for _, part := range append(strings.Split(path[:i], "/"), repoName, provider) {
		// \ and : would be a separator or a volume on Windows
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, "-") || strings.ContainsAny(part, `\:`) {
			return Identity{}, false
		}
	}
	owner := strings.ReplaceAll(path[:i], "/", "-")
	if provider == "bitbucket" && host != "bitbucket.org" {
		owner = strings.ToUpper(owner)
	}
	return Identity{provider, owner, repoName}, true
}

// CloneDest is where id's main clone goes: exactly
// <provider>/<owner>/<repo> inside projectsRoot, compared lexically (see
// CheckCloneParent for symlinks).
func CloneDest(projectsRoot string, id Identity) (string, error) {
	dest := filepath.Join(projectsRoot, filepath.FromSlash(id.Clone()))
	rel, err := filepath.Rel(projectsRoot, dest)
	if err != nil || !fsx.Inside(dest, projectsRoot) || fsx.Depth(rel) != 3 {
		return "", fmt.Errorf("'%s' does not resolve to <provider>/<owner>/<repo> under %s", id.Clone(), projectsRoot)
	}
	return dest, nil
}

// CheckCloneParent refuses dest when its parent folder, symlinks resolved,
// isn't <provider>/<owner> inside projectsRoot (resolved too): a symlinked
// provider or owner folder would have a clone land elsewhere.
func CheckCloneParent(projectsRoot, dest string) error {
	root, err := filepath.EvalSymlinks(projectsRoot)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(dest))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, parent)
	if err != nil || !fsx.Inside(parent, root) || fsx.Depth(rel) != 2 {
		return fmt.Errorf("%s resolves to %s, outside <provider>/<owner> under %s; remove the symlink", filepath.Dir(dest), parent, root)
	}
	return nil
}

// CheckCloneAncestors refuses dest when the deepest part of its parent that
// already exists, symlinks resolved, is outside projectsRoot (resolved too):
// run it before MkdirAll, which would otherwise create the missing folders
// wherever a planted symlink leads.
func CheckCloneAncestors(projectsRoot, dest string) error {
	if _, err := os.Lstat(projectsRoot); err != nil {
		return nil // nothing under it exists to follow; the root's own path is trusted
	}
	root, err := filepath.EvalSymlinks(projectsRoot)
	if err != nil {
		return err
	}
	p := filepath.Dir(dest)
	for {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		next := filepath.Dir(p)
		if next == p {
			return fmt.Errorf("%s: no existing parent folder", dest)
		}
		p = next
	}
	res, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if !fsx.Within(res, root) {
		return fmt.Errorf("%s resolves to %s, outside %s; remove the symlink", p, res, root)
	}
	return nil
}

// Slug names a repo wherever the vault does
// (<provider>-<owner>-<repo>, kebab-case), from <provider>/<owner>/<repo>.
func Slug(clone string) string { return Kebab(strings.ReplaceAll(clone, "/", "-")) }

// Narrow is the folder to start from in root: it goes down the provider
// and owner levels while a level holds exactly one folder (dot-folders
// aside), and never into a repo.
func Narrow(root string) string {
	dir := root
	for depth := 0; depth < 2; depth++ {
		entries, err := os.ReadDir(dir)
		if err != nil {
			break
		}
		var only []string
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				only = append(only, e.Name())
			}
		}
		if len(only) != 1 {
			break
		}
		dir = filepath.Join(dir, only[0])
	}
	return dir
}

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
