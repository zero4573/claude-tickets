package graph

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/container"
)

const indexScript = `set -eu
failed=0
for repo in "$@"; do
  echo "== $repo"
  # From inside the repo, so source_file paths in the graph stay repo-relative
  (cd "$PROJECTS_ROOT/$repo" && graphify update . >/dev/null) || { echo "   failed"; failed=1; }
  [ -f "$PROJECTS_ROOT/$repo/graphify-out/graph.json" ] && echo "   ok" || echo "   no graph (no parseable code?)"
  # graphify keeps a dated snapshot per rebuild day; keep a week of them
  find "$PROJECTS_ROOT/$repo/graphify-out" -mindepth 1 -maxdepth 1 -type d \
    -name '20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]*' -mtime +7 -exec rm -rf {} + 2>/dev/null || true
done
exit "$failed"
`

const IndexLog = "graphify-index"

// ErrIndexFailed: the index run itself failed (Index's failed list is
// empty then).
var ErrIndexFailed = errors.New("indexing failed")

// Index refreshes the graphs (code-only AST pass, incremental) of main
// clones (<provider>/<owner>/<repo> under projectsRoot), building the image
// first if needed. It returns the repos whose graph failed to build; err is
// set when anything failed.
func Index(projectsRoot string, repos []string) (failed []string, err error) {
	tag, err := Build(false)
	if err != nil {
		return nil, fmt.Errorf("the graphify image couldn't be built: %w", err)
	}
	logf, err := os.OpenFile(config.StateLog(IndexLog), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "=== ct graph index %s\n", time.Now().Format(time.RFC3339))
	out := io.MultiWriter(os.Stdout, logf)
	fmt.Fprintf(io.MultiWriter(os.Stderr, logf), "ct graph index: indexing %d repo(s)...\n", len(repos))

	rt, err := container.Runtime()
	if err != nil {
		return nil, err
	}
	args := append([]string{"-i"}, container.Mount(projectsRoot, false)...)
	args = append(args, "-e", "PROJECTS_ROOT="+container.Path(projectsRoot), tag, "sh", "-s", "--")
	argv := container.RunArgs(rt, append(args, repos...)...)
	var buf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(indexScript)
	cmd.Stdout = io.MultiWriter(out, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, logf)
	if runErr := cmd.Run(); runErr != nil {
		repo := ""
		sc := bufio.NewScanner(&buf)
		for sc.Scan() {
			switch line := sc.Text(); {
			case strings.HasPrefix(line, "== "):
				repo = line[3:]
			case line == "   failed":
				failed = append(failed, repo)
			}
		}
		return failed, ErrIndexFailed
	}
	return nil, nil
}
