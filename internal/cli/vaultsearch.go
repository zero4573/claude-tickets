package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/spec"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/vindex"
	"github.com/zero4573/claude-tickets/internal/vsearch"
)

func init() {
	vaultSubcommands = append(vaultSubcommands, vaultSearchCmd, vaultRelatedCmd)
}

// searchVault resolves --vault (default: the current vault).
func searchVault(flag string) (string, error) {
	if flag != "" {
		return vault.Resolve(flag)
	}
	return vault.Current()
}

// loadForSearch updates (unless noUpdate) and loads the index, and opens the
// embedder for its model; the embedder is nil when there is none to use.
func loadForSearch(v string, noUpdate bool) (*vindex.Index, embed.Embedder, error) {
	if !noUpdate {
		if _, err := vindex.Update(v, vindex.UpdateOptions{Log: os.Stderr}); err != nil {
			// A usable index is better than none (read-only vault, full disk).
			if _, serr := os.Stat(filepath.Join(vindex.Dir(v), "meta.json")); serr != nil {
				return nil, nil, err
			}
			fmt.Fprintf(os.Stderr, "ct: vault index not updated, results may be stale: %v\n", err)
		}
	}
	ix, err := vindex.Load(v)
	if err != nil {
		if errors.Is(err, vindex.ErrNoIndex) {
			return nil, nil, fmt.Errorf("no search index in %s; run: ct vault index", v)
		}
		return nil, nil, err
	}
	var emb embed.Embedder
	if ix.Meta.Dims > 0 {
		if emb, err = modelcache.Open(ix.Meta.Model); err != nil {
			emb = nil
		}
	}
	if emb == nil && noUpdate {
		fmt.Fprintln(os.Stderr, vindex.ModelHint)
	}
	return ix, emb, nil
}

func closeEmb(e embed.Embedder) {
	if e != nil {
		_ = e.Close()
	}
}

func vaultSearchCmd() *cobra.Command {
	var (
		vflag, typ, project, tag, mode     string
		k, budget                          int
		paths                              []string
		noLinks, noUpdate, noSpecs, asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "search <query> [--vault v] [--k 10] [--path <folder>]... [--type <t>] [--project <slug>] [--tag <t>] [--budget <tokens>] [--mode hybrid|semantic|keyword] [--no-links] [--no-specs] [--no-update] [--json]",
		Short: "Search the vault: meaning, keywords and links",
		Long: `Ranks the vault's chunks for a query by embedding similarity, BM25 keywords
and links between notes (hybrid, the default). The index is updated first
unless --no-update. Without an embedding model the search is keyword and link
only (ct vault model fetch, on the host, adds it); --mode semantic then fails.

Filters combine with AND; a repeated --path means any of them. With --budget N
results come in rank order while their token estimates fit, each with its full
text (always at least one).

Specs are searched too: the specs/ of each main clone whose specs live in the
repo (read from its default branch, not the working tree; one index per repo in
the cache, rebuilt when that tree changes), and the specs in the vault, which are
notes. A repo's spec is listed as <slug>:specs/<capability>/spec.md. --type spec
finds only specs, --no-specs leaves them out; --path (vault folders) leaves out the
repos' specs.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "" && mode != vsearch.ModeHybrid && mode != vsearch.ModeSemantic && mode != vsearch.ModeKeyword {
				return exitWith(2, fmt.Errorf("unknown --mode %q (hybrid, semantic or keyword)", mode))
			}
			v, err := searchVault(vflag)
			if err != nil {
				return err
			}
			ix, emb, err := loadForSearch(v, noUpdate)
			if err != nil {
				return err
			}
			defer closeEmb(emb)
			query := strings.Join(args, " ")
			opts := vsearch.Options{
				K: k, Paths: paths, Type: typ, Project: project, Tag: tag,
				Budget: budget, Mode: mode, NoLinks: noLinks, NoSpecs: noSpecs,
			}
			resp, err := vsearch.Search(ix, emb, query, opts)
			if errors.Is(err, vsearch.ErrNoModel) {
				return fmt.Errorf("--mode semantic: %s", strings.TrimPrefix(vindex.ModelHint, "ct: "))
			}
			if err != nil {
				return err
			}
			if !noSpecs && len(paths) == 0 && (typ == "" || typ == vindex.SpecType) {
				resp = vsearch.MergeSpecs(resp, searchRepoSpecs(v, noUpdate, ix.Meta.Model, emb, query, opts), k, budget)
			}
			if asJSON {
				return writeJSON(resp)
			}
			for _, r := range resp.Results {
				fmt.Printf("%d. %s › %s  (%.2f)\n", r.Rank, r.Path, strings.Join(r.Heading, " › "), r.Score)
				if budget > 0 {
					for _, l := range strings.Split(strings.TrimRight(r.Text, "\n"), "\n") {
						fmt.Println("   " + l)
					}
				} else {
					fmt.Println("   " + r.Excerpt)
				}
			}
			if resp.Budget != nil && resp.Budget.Truncated {
				fmt.Printf("(budget %d tokens: used %d, more results dropped)\n", resp.Budget.Limit, resp.Budget.Used)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&vflag, "vault", "", "vault (default: the current one)")
	f.IntVar(&k, "k", vsearch.DefaultK, "number of results")
	f.StringArrayVar(&paths, "path", nil, "only notes under this vault folder (repeatable)")
	f.StringVar(&typ, "type", "", "only notes with this frontmatter type")
	f.StringVar(&project, "project", "", "only notes of this project slug")
	f.StringVar(&tag, "tag", "", "only notes with this tag")
	f.IntVar(&budget, "budget", 0, "token budget: results with full text while they fit")
	f.StringVar(&mode, "mode", "hybrid", "hybrid, semantic or keyword")
	f.BoolVar(&noLinks, "no-links", false, "no link boost")
	f.BoolVar(&noSpecs, "no-specs", false, "leave out specs")
	f.BoolVar(&noUpdate, "no-update", false, "don't update the index first")
	f.BoolVar(&asJSON, "json", false, "print JSON")
	return usageExit(cmd)
}

// searchRepoSpecs searches the spec index of every main clone whose specs
// live in the repo (spec.Resolve; vault-located specs are vault notes), after
// bringing each up to date unless noUpdate. A repo that fails is reported and
// left out.
func searchRepoSpecs(v string, noUpdate bool, model string, vaultEmb embed.Embedder, query string, o vsearch.Options) []vsearch.SpecIndexResponse {
	root := vault.LocationsOf(v).ProjectsRoot
	cache := config.CacheDir()
	var out []vsearch.SpecIndexResponse
	for _, clone := range repo.MainClones(root) {
		slug := repo.Slug(clone)
		dir := filepath.Join(root, filepath.FromSlash(clone))
		loc, err := spec.Resolve(v, slug, dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ct: specs of %s not searched: %v\n", slug, err)
			continue
		}
		if loc.Kind != spec.InRepo {
			continue
		}
		if !noUpdate {
			if _, _, err := vindex.UpdateSpecs(cache, slug, dir, vindex.UpdateOptions{Log: os.Stderr}); err != nil {
				fmt.Fprintf(os.Stderr, "ct: spec index of %s not updated, results may be stale: %v\n", slug, err)
			}
		}
		ix, err := vindex.LoadSpecs(cache, slug)
		if err != nil {
			continue
		}
		emb := vaultEmb
		if ix.Meta.Model != model {
			emb = nil
			if ix.Meta.Dims > 0 {
				if e, err := modelcache.Open(ix.Meta.Model); err == nil {
					emb = e
					defer closeEmb(e)
				}
			}
		}
		resp, err := vsearch.Search(ix, emb, query, o)
		if err != nil {
			continue // e.g. --mode semantic with an index without vectors
		}
		out = append(out, vsearch.SpecIndexResponse{Slug: slug, Resp: resp})
	}
	return out
}

func vaultRelatedCmd() *cobra.Command {
	var (
		vflag                                   string
		k                                       int
		includeLinked, chunks, noUpdate, asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "related <note> [--vault v] [--k 10] [--include-linked] [--chunks] [--no-update] [--json]",
		Short: "Notes most like a note",
		Long: `Ranks the notes whose content is closest to <note> (a bare name or a
vault-relative path). The note itself and the notes it links are left out
unless --include-linked; --chunks ranks chunks instead of notes. Without an
embedding model it falls back to keywords from the note's title and headings.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := searchVault(vflag)
			if err != nil {
				return err
			}
			hits := links.NewIndex(v).Resolve(strings.TrimSuffix(filepath.ToSlash(args[0]), ".md"))
			switch len(hits) {
			case 0:
				return exitWith(2, fmt.Errorf("no note %q in %s", args[0], v))
			case 1:
			default:
				var rels []string
				for _, h := range hits {
					r, _ := filepath.Rel(v, h)
					rels = append(rels, filepath.ToSlash(r))
				}
				return exitWith(2, fmt.Errorf("%q is ambiguous: %s", args[0], strings.Join(rels, ", ")))
			}
			rel, _ := filepath.Rel(v, hits[0])
			ix, emb, err := loadForSearch(v, noUpdate)
			if err != nil {
				return err
			}
			defer closeEmb(emb)
			n := vsearch.NoteIndex(ix, filepath.ToSlash(rel))
			if n < 0 {
				return exitWith(2, fmt.Errorf("%s is not in the index (raw/, archive/ and templates/ are not indexed)", filepath.ToSlash(rel)))
			}
			resp, err := vsearch.Related(ix, emb, n, vsearch.RelatedOptions{K: k, IncludeLinked: includeLinked, Chunks: chunks})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(resp)
			}
			for _, r := range resp.Results {
				line := fmt.Sprintf("%d. %s  (%.2f)", r.Rank, r.Path, r.Score)
				if len(r.Heading) > 0 {
					line = fmt.Sprintf("%d. %s › %s  (%.2f)", r.Rank, r.Path, strings.Join(r.Heading, " › "), r.Score)
				}
				if r.Linked {
					line += " linked"
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&vflag, "vault", "", "vault (default: the current one)")
	f.IntVar(&k, "k", vsearch.DefaultK, "number of results")
	f.BoolVar(&includeLinked, "include-linked", false, "keep notes the note links")
	f.BoolVar(&chunks, "chunks", false, "rank chunks instead of notes")
	f.BoolVar(&noUpdate, "no-update", false, "don't update the index first")
	f.BoolVar(&asJSON, "json", false, "print JSON")
	return usageExit(cmd)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
