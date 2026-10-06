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
used-by (projects) or members (groups), and cluster. Read-only.

  --json   the same as JSON
  --check  exit 1 when a groups link isn't a group, a depends-on link isn't
           a project, or a project depends on itself (an empty group is
           only a warning)

Exits 1 on an unknown name, 2 when <vault> isn't a vault.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vaultArg(args[0])
			if err != nil {
				return err
			}
			g, problems := projects.Load(v)
			names := args[1:]
			for i, n := range names {
				if g.Kind(n) == "" {
					return fmt.Errorf("no project or group named %s in %s", n, filepath.Join(v, "projects"))
				}
				names[i] = g.Name(n)
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
			fmt.Fprintf(w, "  bridges: %s\n", listOrNone(c.Bridges))
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
// their clusters. Problems are always the whole vault's.
func groupsJSON(w io.Writer, g *projects.Graph, names []string, problems []projects.Problem) error {
	out := struct {
		Clusters   []projects.Cluster       `json:"clusters"`
		Standalone []string                 `json:"standalone"`
		Projects   map[string]groupsProject `json:"projects"`
		Groups     map[string]groupsGroup   `json:"groups"`
		Problems   []projects.Problem       `json:"problems"`
	}{[]projects.Cluster{}, []string{}, map[string]groupsProject{}, map[string]groupsGroup{}, []projects.Problem{}}
	all := len(names) == 0
	if all {
		names = append(g.Projects(), g.Groups()...)
		out.Clusters = append(out.Clusters, g.Clusters()...)
	}
	seen := map[int]bool{}
	for _, n := range names {
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
