package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/diffcheck"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func wsDiffCheckCmd() *cobra.Command {
	var dir, maxSize, ticket string
	cmd := &cobra.Command{
		Use:   "diff-check [-C <dir>] [--max-size <size>] [--ticket <ID>] <rev-range>",
		Short: "Flag binary or oversized files added by the commits of a range",
		Long: `Checks every non-merge commit of <rev-range> (anything git rev-list
accepts, typically HEAD..task/<branch>) and flags each file it adds or
modifies that git treats as binary or whose blob is larger than --max-size
(a byte count, or a number with K, M or G; default 1M). Deletions are
ignored. One line per flagged file on stdout:

  <binary|large|binary,large>  <size in bytes>  <short sha>  <path>

With --ticket <ID> it also flags the ticket's own vault-only IDs (the ID,
the R-nn of its relay.md, the B-nn of its backlog.md, the finding IDs of
its reviews/: entry lines only) that a commit introduces: in a commit
message any whole-word occurrence, in a file an ID that occurs more often
in the commit's added lines than in its removed ones (an ID kept on a
reworded line passes):

  vault-id  <the ID>  <short sha>  <path | :commit-message>

Lines are per commit in rev-list order, its size lines first.

For a range A..B or A...B it also checks the repo's specs/<capability>/spec.md:
a requirement ID that B adds (not defined at the merge base) but A already
defines, or that B defines twice, is flagged, one line per definition, after
the commit lines (a repo without specs/ is skipped):

  spec-id  <REQ-ID>  <short sha of B>  <spec file>

Exits 0 when clean, 3 when something is flagged, 1 on a usage error, a
directory that is not a git checkout, or a bad range.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := diffcheck.ParseSize(maxSize)
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			if dir == "" {
				dir = "."
			}
			if _, err := exec.LookPath("git"); err != nil {
				return fmt.Errorf("diff-check: git not found")
			}
			var ids []string
			if ticket != "" {
				ctx, err := vault.Require()
				if err != nil {
					return fmt.Errorf("diff-check: %w", err)
				}
				folder := note.TicketDir(ctx.Vault, ticket)
				if st, err := os.Stat(folder); !note.ValidKey(ticket) || err != nil || !st.IsDir() {
					return fmt.Errorf("diff-check: ticket %s not found in the vault", ticket)
				}
				ids = diffcheck.TicketIDs(folder, ticket)
			}
			found, err := diffcheck.Check(dir, args[0], limit)
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			var hits []diffcheck.IDHit
			if ticket != "" {
				if hits, err = diffcheck.CheckIDs(dir, args[0], ids); err != nil {
					return fmt.Errorf("diff-check: %w", err)
				}
			}
			specHits, err := diffcheck.CheckSpecIDs(dir, args[0])
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			// per commit in rev-list order: its size lines, then its ID lines
			revs, err := diffcheck.Revs(dir, args[0])
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			var b strings.Builder
			fi, hi := 0, 0
			for _, sha := range revs {
				short := diffcheck.ShortSHA(sha)
				for ; fi < len(found) && found[fi].Commit == short; fi++ {
					f := found[fi]
					fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", f.Kind, f.Size, f.Commit, f.Path)
				}
				for ; hi < len(hits) && hits[hi].Commit == short; hi++ {
					h := hits[hi]
					fmt.Fprintf(&b, "vault-id\t%s\t%s\t%s\n", h.ID, h.Commit, h.Path)
				}
			}
			if fi != len(found) || hi != len(hits) {
				return fmt.Errorf("diff-check: the range changed while it was checked")
			}
			for _, h := range specHits {
				fmt.Fprintf(&b, "spec-id\t%s\n", h)
			}
			fmt.Fprint(cmd.OutOrStdout(), b.String())
			if len(found) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d file(s) flagged (binary or > %s)\n", len(found), maxSize)
			}
			if len(hits) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d vault-only ID(s) of %s in the range\n", len(hits), ticket)
			}
			if len(specHits) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d requirement ID(s) taken twice in specs/ (renumber the later one)\n", len(specHits))
			}
			if len(found) > 0 || len(hits) > 0 || len(specHits) > 0 {
				return exitError(3)
			}
			return nil
		},
	}
	cmd.SilenceUsage = true
	cmd.Flags().StringVarP(&dir, "dir", "C", "", "the git checkout to inspect (default: the current directory)")
	cmd.Flags().StringVar(&ticket, "ticket", "", "also flag this ticket's vault-only IDs (the ticket must be in the vault)")
	cmd.Flags().StringVar(&maxSize, "max-size", "1M", "size limit: bytes, or a number with K, M or G")
	return cmd
}
