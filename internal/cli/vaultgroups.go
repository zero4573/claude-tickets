package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/projects"
)

func vaultGroupsCmd() *cobra.Command {
	var asJSON, check bool
	cmd := &cobra.Command{
		Use:   "groups <vault> [<project|group>...] [--json] [--check]",
		Short: "Show which projects work together (groups, depends-on) and which are unrelated",
		Long: `Shows how the vault's projects relate. A project note (projects/<x>/<x>.md,
type: project) lists the projects it needs in depends-on and the groups it
belongs to in groups, as wikilinks; a group note (type: group) is optional
and has no members list of its own. Projects and groups linked that way,
directly or not, form a cluster: they're related. Projects in different
clusters are unrelated, and a project without links stands alone.

Without names: every cluster (its groups, projects and the bridges between
its groups, or its depends-on links when it has no group), the standalone
projects, then the problems. With names: each one's groups, depends-on,
used-by (projects) or members (groups), and cluster. A name without a
project or group note (e.g. a repo not saved to the vault yet) is reported
as "<name>: no note (standalone)". Read-only.

  --json   the same as JSON ("noNote" lists the names without a note)
  --check  exit 1 when a groups link isn't a group, a depends-on link isn't
           a project, or a project depends on itself (an empty group is
           only a warning). With names, only the named notes' problems
           count; without, the whole vault's.

Exits 2 when <vault> isn't a vault.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vaultArg(args[0])
			if err != nil {
				return err
			}
			g, problems := projects.Load(v)
			names := args[1:]
			if len(names) > 0 {
				problems = problemsOf(g, names, problems)
			}
			if asJSON {
				if err := groupsJSON(os.Stdout, g, names, problems); err != nil {
					return err
				}
			} else if len(names) > 0 {
				groupsNamed(os.Stdout, g, names)
				if check {
					printProblems(os.Stdout, problems)
				}
			} else {
				groupsAll(os.Stdout, g)
				printProblems(os.Stdout, problems)
			}
			if check {
				for _, p := range problems {
					if p.Error {
						return exitError(1)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&check, "check", false, "exit 1 if the links between projects and groups have errors")
	return cmd
}

// problemsOf keeps the problems found in the named notes.
func problemsOf(g *projects.Graph, names []string, problems []projects.Problem) []projects.Problem {
	files := map[string]bool{}
	for _, n := range names {
		if f := g.File(n); f != "" {
			files[f] = true
		}
	}
	var out []projects.Problem
	for _, p := range problems {
		if files[p.File] {
			out = append(out, p)
		}
	}
	return out
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

func clusterSummary(c projects.Cluster) string {
	s := "projects " + strings.Join(c.Projects, ", ")
	if len(c.Groups) > 0 {
		s = "groups " + strings.Join(c.Groups, ", ") + "; " + s
	}
	return s
}

func groupsAll(w io.Writer, g *projects.Graph) {
	if len(g.Projects()) == 0 {
		fmt.Fprintln(w, "no projects")
		return
	}
	for i, c := range g.Clusters() {
		fmt.Fprintf(w, "cluster %d: %s\n", i+1, clusterSummary(c))
		if len(c.Groups) > 0 {
			// One per line: a bridge's own text has commas
			if len(c.Bridges) == 0 {
				fmt.Fprintln(w, "  bridges: none")
			} else {
				fmt.Fprintln(w, "  bridges:")
				for _, b := range c.Bridges {
					fmt.Fprintf(w, "    %s\n", b)
				}
			}
			continue
		}
		for _, p := range c.Projects {
			for _, d := range g.DependsOn(p) {
				fmt.Fprintf(w, "  %s depends-on %s\n", p, d)
			}
		}
	}
	fmt.Fprintf(w, "standalone: %s\n", listOrNone(g.Standalone()))
}

func groupsNamed(w io.Writer, g *projects.Graph, names []string) {
	for _, n := range names {
		kind := g.Kind(n)
		if kind == "" {
			fmt.Fprintf(w, "%s: no note (standalone)\n", n)
			continue
		}
		n = g.Name(n)
		fmt.Fprintf(w, "%s (%s)\n", n, kind)
		if kind == projects.KindGroup {
			fmt.Fprintf(w, "  members: %s\n", listOrNone(g.Members(n)))
		} else {
			fmt.Fprintf(w, "  groups: %s\n", listOrNone(g.GroupsOf(n)))
			fmt.Fprintf(w, "  depends-on: %s\n", listOrNone(g.DependsOn(n)))
			fmt.Fprintf(w, "  used-by: %s\n", listOrNone(g.UsedBy(n)))
		}
		switch i, ok := g.ClusterOf(n); {
		case ok:
			fmt.Fprintf(w, "  cluster: %s\n", clusterSummary(g.Clusters()[i]))
		case kind == projects.KindGroup:
			fmt.Fprintln(w, "  cluster: none (no members)")
		default:
			fmt.Fprintln(w, "  cluster: standalone")
		}
	}
}

func printProblems(w io.Writer, problems []projects.Problem) {
	for _, p := range problems {
		level := "warning"
		if p.Error {
			level = "error"
		}
		fmt.Fprintf(w, "%s: %s (%s)\n", level, p.Message, filepath.ToSlash(p.File))
	}
}

type groupsProject struct {
	Groups    []string `json:"groups"`
	DependsOn []string `json:"dependsOn"`
	UsedBy    []string `json:"usedBy"`
}

type groupsGroup struct {
	Members []string `json:"members"`
}

// groupsJSON writes the whole model, or with names only those names and
// their clusters. Names without a note go in noNote (and standalone).
func groupsJSON(w io.Writer, g *projects.Graph, names []string, problems []projects.Problem) error {
	out := struct {
		Clusters   []projects.Cluster       `json:"clusters"`
		Standalone []string                 `json:"standalone"`
		NoNote     []string                 `json:"noNote"`
		Projects   map[string]groupsProject `json:"projects"`
		Groups     map[string]groupsGroup   `json:"groups"`
		Problems   []projects.Problem       `json:"problems"`
	}{[]projects.Cluster{}, []string{}, []string{}, map[string]groupsProject{}, map[string]groupsGroup{}, []projects.Problem{}}
	all := len(names) == 0
	if all {
		names = append(g.Projects(), g.Groups()...)
		out.Clusters = append(out.Clusters, g.Clusters()...)
	}
	seen := map[int]bool{}
	for _, n := range names {
		if g.Kind(n) == "" {
			out.NoNote = append(out.NoNote, n)
			out.Standalone = append(out.Standalone, n)
			continue
		}
		n = g.Name(n)
		i, ok := g.ClusterOf(n)
		if ok && !all && !seen[i] {
			seen[i] = true
			out.Clusters = append(out.Clusters, g.Clusters()[i])
		}
		if g.Kind(n) == projects.KindGroup {
			out.Groups[n] = groupsGroup{g.Members(n)}
			continue
		}
		out.Projects[n] = groupsProject{g.GroupsOf(n), g.DependsOn(n), g.UsedBy(n)}
		if !ok {
			out.Standalone = append(out.Standalone, n)
		}
	}
	for _, p := range problems {
		p.File = filepath.ToSlash(p.File)
		out.Problems = append(out.Problems, p)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
