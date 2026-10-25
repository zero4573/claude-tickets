package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/vindex"
)

func init() { vaultSubcommands = append(vaultSubcommands, vaultIndexCmd) }

func vaultIndexCmd() *cobra.Command {
	var full, asJSON bool
	var model string
	var chunkTokens int
	cmd := &cobra.Command{
		Use:   "index [<vault>] [--full] [--model <id>] [--chunk-tokens N] [--json]",
		Short: "Build or update the vault's search index",
		Long: `Builds or incrementally updates the search index in <vault>/.ct/index: every
note outside dot-folders, raw/, archive/ and templates/ is split into chunks and
embedded with the local model (ct vault model). Only new and changed notes are
read again, and chunks that did not change keep their vectors. Without a model
the index is keyword-only (ct vault model fetch, on the host, adds it).

  --full           rebuild everything
  --model <id>     embed with this pinned model (a change rebuilds the index)
  --chunk-tokens   chunk size limit in tokens (default 256)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var v string
			var err error
			if len(args) > 0 {
				v, err = vault.Resolve(args[0])
			} else {
				v, err = vault.Current()
			}
			if err != nil {
				return err
			}
			if _, ok := modelcache.Lookup(model); model != "" && !ok && os.Getenv("CLAUDE_TICKETS_EMBEDDER") == "" {
				return exitWith(2, fmt.Errorf("unknown model %q (ct vault model lists the pinned ones)", model))
			}
			st, err := vindex.Update(v, vindex.UpdateOptions{
				Full: full, Model: model, ChunkTokens: chunkTokens, Log: os.Stderr,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"notes": st.Notes, "changed": st.Changed, "removed": st.Removed,
					"chunks": st.Chunks, "embedded": st.Embedded, "model": st.Model,
					"keyword_only": st.KeywordOnly, "took_ms": st.Took.Milliseconds(),
				})
			}
			m := "(" + st.Model + ")"
			if st.KeywordOnly {
				m = "(keyword only)"
			}
			fmt.Printf("indexed %d notes: %d changed, %d removed, %d chunks embedded %s in %.1fs\n",
				st.Notes, st.Changed, st.Removed, st.Embedded, m, st.Took.Seconds())
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "rebuild the whole index")
	cmd.Flags().StringVar(&model, "model", "", "embedding model id")
	cmd.Flags().IntVar(&chunkTokens, "chunk-tokens", 0, "chunk size limit in tokens")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return usageExit(cmd)
}
