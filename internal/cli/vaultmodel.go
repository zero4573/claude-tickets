package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/modelcache"
)

func init() { vaultSubcommands = append(vaultSubcommands, vaultModelCmd) }

func vaultModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage the local embedding model used by ct vault search",
		Long: `The local embedding model (and the llama.cpp library that runs it) behind
ct vault index and ct vault search. Both are pinned, downloaded once on the
host into ` + config.TildePath(modelcache.Dir()) + ` and verified by sha256; sessions
get that directory read-only. Without them, search is keyword-only.

  ct vault model status [<id>] [--json]   what is present
  ct vault model fetch [<id>]             download and verify (on the host)
  ct vault model list [--json]            the pinned model ids`,
	}
	cmd.AddCommand(usageExit(vaultModelStatusCmd()), usageExit(vaultModelFetchCmd()), usageExit(vaultModelListCmd()))
	return usageExit(cmd)
}

func modelID(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return modelcache.Default().ID
}

func vaultModelStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status [<id>] [--json]",
		Short: "Show whether the model and the llama.cpp library are present",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := modelID(args)
			if _, ok := modelcache.Lookup(id); !ok {
				return exitWith(2, fmt.Errorf("unknown model %q (ct vault model list)", id))
			}
			st := modelcache.StatusOf(id)
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"model": st.Model.ID, "dims": st.Model.Dims,
					"model_path": st.ModelPath, "model_ok": st.ModelOK,
					"lib_dir": st.LibDir, "lib_ok": st.LibOK, "problem": st.Problem,
				})
			}
			fmt.Printf("model:    %s (%d dims)\n", st.Model.ID, st.Model.Dims)
			fmt.Printf("file:     %s  %s\n", config.TildePath(st.ModelPath), presence(st.ModelOK))
			lib := st.LibDir
			if lib == "" {
				lib = "-"
			} else {
				lib = config.TildePath(lib)
			}
			fmt.Printf("library:  %s  %s\n", lib, presence(st.LibOK))
			if st.Problem != "" {
				fmt.Printf("problem:  %s\n", st.Problem)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func presence(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

func vaultModelFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch [<id>]",
		Short: "Download and verify the model and the llama.cpp library (on the host)",
		Long: `Downloads the pinned model and the pinned llama.cpp build for this platform
into the cache, checks their sha256 and installs them atomically. What is
already present is not downloaded again. Refuses inside a container: run it
on the host, sessions then get the directory read-only.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := modelID(args)
			if _, ok := modelcache.Lookup(id); !ok {
				return exitWith(2, fmt.Errorf("unknown model %q (ct vault model list)", id))
			}
			if gitx.InContainer() {
				return errors.New("ct vault model fetch writes the host cache: run it on the host")
			}
			if err := modelcache.Fetch(ctxOf(cmd), id, os.Stdout); err != nil {
				return err
			}
			fmt.Printf("ready: %s\n", config.TildePath(modelcache.Dir()))
			return nil
		},
	}
}

func vaultModelListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [--json]",
		Short: "List the pinned model ids",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			def := modelcache.Default().ID
			if asJSON {
				type row struct {
					ID      string `json:"id"`
					Dims    int    `json:"dims"`
					Default bool   `json:"default"`
				}
				rows := []row{}
				for _, id := range modelcache.IDs() {
					m, _ := modelcache.Lookup(id)
					rows = append(rows, row{id, m.Dims, id == def})
				}
				return json.NewEncoder(os.Stdout).Encode(rows)
			}
			for _, id := range modelcache.IDs() {
				m, _ := modelcache.Lookup(id)
				mark := ""
				if id == def {
					mark = " (default)"
				}
				fmt.Printf("%s  %d dims%s\n", id, m.Dims, mark)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func ctxOf(cmd *cobra.Command) context.Context {
	if c := cmd.Context(); c != nil {
		return c
	}
	return context.Background()
}
