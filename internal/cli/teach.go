package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/prompt"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// topicRe is a learning topic's name: kebab-case, as it names the folder
// learning/<topic>/ and prefixes every file in it.
var topicRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func teachCmd() *cobra.Command {
	var cont, list bool
	cmd := &cobra.Command{
		Use:   "teach [--continue] [<topic>] | --list",
		Short: "Learn a topic over many sessions, kept in the vault's learning/<topic>/",
		Long: `Opens a teaching session on a topic (kebab-case, e.g. rust-async), from the
current vault (ct vault default): a Claude session running /tickets:teach
<topic>, in this terminal, in the foreground. A topic whose session runs
in another terminal isn't started again.

The session works in <workRoot>/.teach-<vault>/<topic>, whose CLAUDE.md
names the vault and the topic's folder, with:
  * the vault, where it writes only learning/<topic>/ (under the vault lock)
  * every repo in its projectsRoot, read-only, for examples (not fetched)
  * one code graph (graphify) merged from all the main clones
  * the MCP servers your claude command provides
The teach skill keeps the mission, resources, glossary, notes, lessons,
reference notes, learning records and quizzes there, so each session picks
up where the last one left off.

Without a topic it lists learning/*/ and asks which one, or a new one; with
no terminal it prints them and fails. --list prints the topics. Each session
is a new conversation; --continue (-c) continues the topic's last one
instead.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTopics,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			topics := learningTopics(ctx.Vault)
			if list {
				for _, t := range topics {
					fmt.Println(t)
				}
				return nil
			}
			topic := ""
			if len(args) == 1 {
				topic = args[0]
			} else if topic, err = pickTopic(topics); err != nil {
				return err
			}
			if !topicRe.MatchString(topic) {
				return fmt.Errorf("not a kebab-case topic: '%s' (lowercase letters, digits and dashes, e.g. rust-async)", topic)
			}
			return teach(ctx, topic, cont)
		},
	}
	cmd.Flags().BoolVarP(&cont, "continue", "c", false, "continue the topic's last conversation instead of starting a new one")
	cmd.Flags().BoolVar(&list, "list", false, "print the vault's learning topics")
	return cmd
}

// learningTopics is every folder in the vault's learning/, sorted.
func learningTopics(vaultDir string) []string {
	entries, _ := os.ReadDir(filepath.Join(vaultDir, "learning"))
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func completeTopics(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, err := vault.Require()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return learningTopics(ctx.Vault), cobra.ShellCompDirectiveNoFileComp
}

// pickTopic asks for a topic: one of topics by number, or a new name. With
// no terminal to ask in, it prints them and fails.
func pickTopic(topics []string) (string, error) {
	if !isTerminal() {
		for _, t := range topics {
			fmt.Println(t)
		}
		return "", errors.New("name a topic: ct teach <topic> (kebab-case; one above, or a new one)")
	}
	for i, t := range topics {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, t)
	}
	q := "New topic (kebab-case):"
	if len(topics) > 0 {
		q = "Topic (a number, or a new kebab-case name):"
	}
	a := strings.TrimSpace(prompt.Line(q))
	if n, err := strconv.Atoi(a); err == nil {
		if n < 1 || n > len(topics) {
			return "", fmt.Errorf("not a choice: %s", a)
		}
		return topics[n-1], nil
	}
	if a == "" {
		return "", errors.New("no topic given")
	}
	return a, nil
}

func teach(ctx vault.Context, topic string, cont bool) error {
	if !isTerminal() {
		return errors.New("the session runs in this terminal, and this isn't one (stdin/stdout); run it from a terminal")
	}
	name := "teach-" + topic

	// A workspace with no repos of its own, like kb's: its graph (ct graph
	// mcp) merges every main clone, and the hooks record its state there.
	// Hidden, so ct status and ct ws skip it.
	dir := filepath.Join(ctx.WorkRoot, ".teach-"+filepath.Base(ctx.Vault), topic)
	_, err := os.Stat(filepath.Join(dir, ".agent-state"))
	resume := err == nil && cont
	if cont && !resume {
		warnf("%s: no earlier session on this topic to continue, starting a new one", topic)
	}
	if err := workspace.Ensure(dir, name, ctx.Vault); err != nil {
		return err
	}
	if err := session.Trust(dir); err != nil {
		warnf("couldn't mark %s as trusted in %s: %v", dir, session.ClaudeConfig(), err)
	}
	if err := session.WriteSettings(dir, append(session.ReposReadOnly(ctx.ProjectsRoot), "Bash(git commit:*)", "Bash(git push:*)")...); err != nil {
		return err
	}
	writeToolVersions(ctx, dir)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(teachClaudeMD(ctx, topic)), 0o644); err != nil {
		return err
	}
	argv, err := session.Command(ctx.Vault, ctx.ProjectsRoot, filepath.Join(config.CacheDir(), "graphify"), modelcache.Dir())
	if err != nil {
		return err
	}
	if _, err := graph.Build(false); err == nil {
		mcp, err := session.WriteGraphMCP(dir, ctx.Vault)
		if err != nil {
			return err
		}
		argv = append(argv, "--mcp-config", mcp)
	} else {
		warnf("%v", err)
		warnf("the graphify image couldn't be built; starting without the code graph")
	}
	argv = append(argv, "--permission-mode", "auto")
	if resume {
		argv = append(argv, "--continue")
	}
	argv = append(argv, "/tickets:teach "+topic)
	w := launcher.Session{Dir: dir, Argv: argv, Env: map[string]string{"CLAUDE_TICKETS_VAULT": ctx.Vault}}
	again := ""
	if resume {
		again = " (continuing its last session)"
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return err
	}
	// One session per topic: the teach skill writes its folder directly
	if err := workspace.ClaimSession(dir, "teach"); err != nil {
		var running *workspace.RunningError
		if errors.As(err, &running) {
			return fmt.Errorf("%s: a teach session on it is running (pid %d) in another terminal; continue there, or end it first", topic, running.Record.PID)
		}
		warnf("couldn't record the session in %s: %v", workspace.SessionsFile(dir), err)
	}
	fmt.Printf("ct teach: %s runs in this terminal%s\n", topic, again)
	return passExit(launcher.Run(w))
}

func teachClaudeMD(ctx vault.Context, topic string) string {
	return fmt.Sprintf("# Teaching session: %[1]s\n\n"+
		"Written by ct teach; regenerated on every start, so don't edit it.\n\n"+
		"- Vault: `%[2]s`. Read its `AGENTS.md` for note rules.\n"+
		"- Topic: `%[1]s`. This session writes **only** `%[2]s/learning/%[1]s/`,\n"+
		"  directly (no `/tickets:save`), holding `ct vault lock acquire %[2]s\n"+
		"  teach-%[1]s` around writes that touch more than one file.\n"+
		"- Repos: main clones at `%[3]s/<provider>/<owner>/<repo>`, read-only, for\n"+
		"  examples. Never edit, commit or push.\n"+
		"- Code graph: the `graphify` MCP server holds every repo, with node ids\n"+
		"  prefixed by the repo's slug.\n\n"+
		"Follow the `teach` skill (`/tickets:teach %[1]s`). `ct teach %[1]s` opens a new\n"+
		"conversation, which picks up from the topic's notes; `ct teach --continue\n"+
		"%[1]s` reopens the last one.\n",
		topic, ctx.Vault, ctx.ProjectsRoot)
}
