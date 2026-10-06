package projects

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// proj writes projects/<name>/<name>.md with the given type and extra
// frontmatter lines.
func proj(t *testing.T, v, name, kind, fm string) {
	t.Helper()
	p := filepath.Join(v, "projects", name, name+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\ntitle: "+name+"\ntype: "+kind+"\n"+fm+"---\n# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func errorsOf(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		if p.Error {
			out = append(out, p.Message)
		}
	}
	return out
}

func TestStandaloneAndPair(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "web", "project", "groups: []\ndepends-on: []\n")
	proj(t, v, "cli", "project", "")
	proj(t, v, "nix", "project", "depends-on: [\"[[cli]]\"]\n")
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "Projects", g.Projects(), []string{"cli", "nix", "web"})
	eq(t, "Standalone", g.Standalone(), []string{"web"})
	eq(t, "Clusters", g.Clusters(), []Cluster{{Groups: []string{}, Projects: []string{"cli", "nix"}, Bridges: []string{}}})
	eq(t, "DependsOn(nix)", g.DependsOn("nix"), []string{"cli"})
	eq(t, "UsedBy(cli)", g.UsedBy("CLI"), []string{"nix"})
	if _, ok := g.ClusterOf("web"); ok {
		t.Error("web has a cluster")
	}
	if i, ok := g.ClusterOf("nix"); !ok || i != 0 {
		t.Errorf("ClusterOf(nix) = %d, %v", i, ok)
	}
}

func TestGroups(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "alpha", "group", "")
	proj(t, v, "beta", "group", "")
	proj(t, v, "zeta", "group", "")
	proj(t, v, "p1", "project", "groups: [\"[[alpha]]\"]\n")
	// Block style, alias and heading, other case
	proj(t, v, "p2", "project", "groups:\n  - \"[[Alpha|the alpha]]\"\ndepends-on:\n  - '[[p1#API]]'\n")
	proj(t, v, "q1", "project", "groups: [\"[[beta]]\"]\n")
	proj(t, v, "q2", "project", "groups: [\"[[projects/beta/beta]]\"]\n")
	g, ps := Load(v)
	eq(t, "errors", errorsOf(ps), []string(nil))
	eq(t, "warnings", ps, []Problem{{File: filepath.Join("projects", "zeta", "zeta.md"), Message: "group zeta has no members"}})
	eq(t, "Groups", g.Groups(), []string{"alpha", "beta", "zeta"})
	eq(t, "Members(alpha)", g.Members("alpha"), []string{"p1", "p2"})
	eq(t, "GroupsOf(p2)", g.GroupsOf("p2"), []string{"alpha"})
	eq(t, "DependsOn(p2)", g.DependsOn("p2"), []string{"p1"})
	// Two unrelated groups: two clusters
	eq(t, "Clusters", g.Clusters(), []Cluster{
		{Groups: []string{"alpha"}, Projects: []string{"p1", "p2"}, Bridges: []string{}},
		{Groups: []string{"beta"}, Projects: []string{"q1", "q2"}, Bridges: []string{}},
	})
	eq(t, "Standalone", g.Standalone(), []string{})
	if _, ok := g.ClusterOf("zeta"); ok {
		t.Error("an empty group has a cluster")
	}
}

func TestBridges(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "a", "group", "")
	proj(t, v, "b", "group", "")
	proj(t, v, "c", "group", "")
	proj(t, v, "p", "project", "groups: [\"[[a]]\"]\n")
	proj(t, v, "q", "project", "groups: [\"[[a]]\", \"[[b]]\"]\n")
	proj(t, v, "r", "project", "groups: [\"[[c]]\"]\ndepends-on: [\"[[p]]\"]\n")
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "Clusters", g.Clusters(), []Cluster{{
		Groups:   []string{"a", "b", "c"},
		Projects: []string{"p", "q", "r"},
		Bridges:  []string{"q is in groups a, b", "r (c) depends-on p (a)"},
	}})
}

func TestProblems(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "grp", "group", "")
	proj(t, v, "cli", "project", "groups: [\"[[grp]]\"]\n")
	proj(t, v, "web", "project", "groups: [\"[[cli]]\", \"[[nowhere]]\"]\ndepends-on: [\"[[grp]]\", \"[[missing]]\", \"[[web]]\", \"[[AGENTS]]\"]\n")
	// Ignored: a folder without an index note, a note with another type
	os.MkdirAll(filepath.Join(v, "projects", "empty", "sequences"), 0o755)
	proj(t, v, "notes", "reference", "depends-on: [\"[[missing]]\"]\n")
	os.WriteFile(filepath.Join(v, "projects", "cli", "other.md"), []byte("---\ntype: project\n---\n"), 0o644)
	g, ps := Load(v)
	eq(t, "errors", errorsOf(ps), []string{
		"depends-on links [[grp]], which is a group, not a project",
		"depends-on links [[missing]], which is not a project note",
		"depends-on links web itself",
		"depends-on links [[AGENTS]], which is not a project note",
		"groups links [[cli]], which is a project, not a group",
		"groups links [[nowhere]], which is not a group note",
	})
	for _, p := range ps {
		if p.Error && p.File != filepath.Join("projects", "web", "web.md") {
			t.Errorf("problem file = %s", p.File)
		}
	}
	eq(t, "Projects", g.Projects(), []string{"cli", "web"})
	eq(t, "Standalone", g.Standalone(), []string{"web"})
	eq(t, "DependsOn(web)", g.DependsOn("web"), []string{})
	eq(t, "unknown", g.Kind("other"), "")
}

func TestEmptyVault(t *testing.T) {
	g, ps := Load(t.TempDir())
	eq(t, "problems", len(ps), 0)
	eq(t, "Projects", g.Projects(), []string{})
	eq(t, "Clusters", len(g.Clusters()), 0)
}

func TestTarget(t *testing.T) {
	for in, want := range map[string]string{
		"[[x]]":                   "x",
		"[[X|alias]]":             "x",
		"[[x#Heading]]":           "x",
		"[[projects/x/x.md|y]]":   "x",
		" x ":                     "x",
		"[[x^block]]":             "x",
		"[[Github-Acme-Cli#a|b]]": "github-acme-cli",
	} {
		if got := Target(in); got != want {
			t.Errorf("Target(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Contains(Target("[[a/b]]"), "/") {
		t.Error("path kept")
	}
}

// A two-way interaction is a link on each side; repeated links to the same
// project (any case, alias or heading) count once.
func TestCyclesAndDuplicates(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "a", "project", "depends-on: [\"[[b]]\", \"[[B]]\", \"[[b|the b]]\", \"[[b#API]]\"]\n")
	proj(t, v, "b", "project", "depends-on:\n  - \"[[a]]\"\n")
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "DependsOn(a)", g.DependsOn("a"), []string{"b"})
	eq(t, "UsedBy(b)", g.UsedBy("b"), []string{"a"})
	eq(t, "UsedBy(a)", g.UsedBy("a"), []string{"b"})
	eq(t, "Clusters", g.Clusters(), []Cluster{{Groups: []string{}, Projects: []string{"a", "b"}, Bridges: []string{}}})
	eq(t, "Standalone", g.Standalone(), []string{})
}

// Two groups joined through a project in neither are one cluster (they're
// related), but that chain isn't listed as a bridge.
func TestGroupsJoinedThroughUngroupedProject(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "g1", "group", "")
	proj(t, v, "g2", "group", "")
	proj(t, v, "p", "project", "groups: [\"[[g1]]\"]\ndepends-on: [\"[[lib]]\"]\n")
	proj(t, v, "q", "project", "groups: [\"[[g2]]\"]\ndepends-on: [\"[[lib]]\"]\n")
	proj(t, v, "lib", "project", "")
	proj(t, v, "other", "project", "")
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "Clusters", g.Clusters(), []Cluster{{Groups: []string{"g1", "g2"}, Projects: []string{"lib", "p", "q"}, Bridges: []string{}}})
	i, ok := g.ClusterOf("G2")
	if j, ok2 := g.ClusterOf("p"); !ok || !ok2 || i != j {
		t.Errorf("g2 and p not in one cluster: %d %v, %d %v", i, ok, j, ok2)
	}
	eq(t, "Standalone", g.Standalone(), []string{"other"})
}

// Groups are flat and have no members list: a group note's own groups,
// depends-on (or members) properties are ignored, so a group never nests
// in another or joins a cluster on its own.
func TestGroupNoteLinksIgnored(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "outer", "group", "")
	proj(t, v, "inner", "group", "groups: [\"[[outer]]\"]\ndepends-on: [\"[[p]]\"]\nmembers: [\"[[p]]\"]\n")
	proj(t, v, "p", "project", "")
	g, ps := Load(v)
	eq(t, "errors", errorsOf(ps), []string(nil))
	eq(t, "warnings", len(ps), 2)
	eq(t, "Members(inner)", g.Members("inner"), []string{})
	eq(t, "Members(outer)", g.Members("outer"), []string{})
	eq(t, "Clusters", len(g.Clusters()), 0)
	eq(t, "Standalone", g.Standalone(), []string{"p"})
}

// No legacy: the old projects/system/system.md (type: project, no repo)
// of a vault set up before 0.1.0 is just a standalone project named
// system; its old services property means nothing.
func TestOldSystemNoteIsAStandaloneProject(t *testing.T) {
	v := t.TempDir()
	proj(t, v, "system", "project", "tags: [project, system, microservices]\nservices: [\"[[a]]\", \"[[b]]\"]\n")
	proj(t, v, "a", "project", "")
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "Standalone", g.Standalone(), []string{"a", "system"})
	eq(t, "Clusters", len(g.Clusters()), 0)
}

// A projects path that isn't a folder is a vault without projects.
func TestProjectsNotAFolder(t *testing.T) {
	v := t.TempDir()
	if err := os.WriteFile(filepath.Join(v, "projects"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	g, ps := Load(v)
	eq(t, "problems", len(ps), 0)
	eq(t, "Projects", g.Projects(), []string{})
}
