package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/projects"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func claudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude [claude args...]",
		Short: "Claude in the current repo, with the vault as its knowledge base",
		Long: `Runs claude in the current repo with the current vault (ct vault default)
as its knowledge base: the vault is added (--add-dir), the ticket plugin's
skills are loaded (/tickets:kb, /tickets:recall, /tickets:save), and the
system prompt names the vault and this repo's notes in it. The arguments
are claude's own. Without ct claude a session knows nothing of the vault.`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
				return cmd.Help()
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(ctx.Vault, "AGENTS.md")); err != nil {
				return fmt.Errorf("%s has no AGENTS.md (set it up with ct vault init)", ctx.Vault)
			}
			wd, _ := os.Getwd()
			argv, err := session.Command(ctx.Vault)
			if err != nil {
				return err
			}
			argv = append(append(argv, "--append-system-prompt", vaultPrompt(ctx, repoSlugAt(ctx, wd))), args...)
			path, err := exec.LookPath(argv[0])
			if err != nil {
				return err
			}
			return passExit(execReplaceOrRun(path, argv))
		},
	}
	return cmd
}

// repoSlugAt takes the slug from dir's path under the projects root, else
// from its origin ("" outside a repo).
func repoSlugAt(ctx vault.Context, dir string) string {
	top, err := gitx.Out(dir, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return ""
	}
	root := ctx.ProjectsRoot
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if rel, err := filepath.Rel(root, top); err == nil && fsx.Inside(top, root) && fsx.Depth(rel) == 3 {
		return repo.Slug(filepath.ToSlash(rel))
	}
	url, _ := gitx.Out(top, "remote", "get-url", "origin")
	if id, ok := repo.RemoteIdentity(url); ok {
		return repo.Slug(id.Clone())
	}
	return ""
}

func vaultPrompt(ctx vault.Context, slug string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Knowledge base\n\nThis session's knowledge base is the Obsidian vault at `%s` (added to\nthis session, read-write). Read its `AGENTS.md` before writing anything there.\n", ctx.Vault)
	if slug != "" {
		if _, err := os.Stat(filepath.Join(ctx.Vault, "projects", slug, slug+".md")); err == nil {
			fmt.Fprintf(&b, "- This repo is `%[1]s`. Its notes are in `projects/%[1]s/`: the index `%[1]s.md`, `architecture/%[1]s-decisions.md`, `features/`, `sequences/`, `logs/`.\n", slug)
			relationsPrompt(&b, ctx.Vault, slug)
		} else {
			fmt.Fprintf(&b, "- This repo is `%[1]s`. It has no notes yet; `/tickets:save` creates `projects/%[1]s/`.\n", slug)
		}
	}
	b.WriteString("- `knowledge-base/` and `references/` hold findings outside any project; `tickets/` holds the history of past work.\n" +
		"- For questions about this repo or the projects it works with, and before non-trivial changes, use the `kb` skill. Check the vault first, then the code graph, then the code.\n" +
		"- Don't edit `projects/`, `knowledge-base/` or `references/` directly. Record what you learn as drafts in `inbox/` (see the `kb` skill); `/tickets:save` promotes them at the end of the session.\n" +
		"- `/tickets:recall` loads this repo's recent history and decisions. Run `/tickets:save` before ending the session.")
	return b.String()
}

// relationsPrompt names the projects the repo works with, as its note and
// the other project notes say (projects.Load); nothing for a standalone repo.
func relationsPrompt(b *strings.Builder, v, slug string) {
	g, _ := projects.Load(v)
	if g.Kind(slug) != projects.KindProject {
		return
	}
	deps, users, groups := g.DependsOn(slug), g.UsedBy(slug), g.GroupsOf(slug)
	if len(deps)+len(users)+len(groups) == 0 {
		return
	}
	quote := func(l []string) string { return "`" + strings.Join(l, "`, `") + "`" }
	if len(deps) > 0 {
		fmt.Fprintf(b, "- It depends on %s.\n", quote(deps))
	}
	if len(users) > 0 {
		verb := "depend"
		if len(users) == 1 {
			verb = "depends"
		}
		fmt.Fprintf(b, "- %s %s on it.\n", quote(users), verb)
	}
	for _, grp := range groups {
		fmt.Fprintf(b, "- It's in the group `%[1]s` (`projects/%[1]s/`: interactions, compatibility, decisions, flows across its members).\n", grp)
	}
	// Quoted: the vault path may have spaces
	fmt.Fprintf(b, "- Run `ct vault groups \"%s\" %s` for the projects it works with.\n", v, slug)
}
