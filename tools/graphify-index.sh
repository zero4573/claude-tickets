usage() {
  cat <<'EOF2'
Usage: graphify-index [<repo>...]

Builds or refreshes the code graph (graphify-out/graph.json, code-only AST
pass, no LLM, incremental) of each main clone under
~/Projects/<provider>/<owner>/<repo>: all of them, or the given ones (a
<repo> is a slug or <provider>/<owner>/<repo>). Ticket sessions merge these
graphs for every repo the ticket has no worktree of. Runs graphify in the
claude-tickets graphify image (ticket-graph build; podman or docker).
Output is also logged to ~/.local/state/graphify-index.log.
EOF2
}
[[ "${1:-}" == -h || "${1:-}" == --help ]] && { usage; exit 0; }

repos=()
for spec in "$@"; do
  clone="$(resolve_repo "$spec")" || die "no main clone '$spec' (see ticket-ws repos)"
  repos+=("$clone")
done
[[ ${#repos[@]} -gt 0 ]] || mapfile -t repos < <(list_main_clones)
[[ ${#repos[@]} -gt 0 ]] || die "no main clones under $projects_root (expected <provider>/<owner>/<repo>; see repo-layout)"
image="$(ticket-graph build)" || die "the graphify image couldn't be built"

# Everything below also goes to the log
log="$(state_log graphify-index)"
echo "=== graphify-index $(date -Iseconds)" >> "$log"
exec > >(tee -a "$log") 2>&1
echo "graphify-index: indexing ${#repos[@]} repo(s)..." >&2
out="$(mktemp)"
trap 'rm -f "$out"' EXIT
set +e
container_run -i \
  -v "$projects_root:$projects_root" \
  -e PROJECTS_ROOT="$projects_root" \
  "$image" \
  sh -s -- "${repos[@]}" <<'SCRIPT' | tee "$out"
set -eu
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
SCRIPT
code="${PIPESTATUS[0]}"
set -e

# Repos whose build failed become tasks in the default vault's inbox/
if [[ "$code" != 0 && -n "${CLAUDE_TICKETS_VAULT:-}" ]]; then
  tasks="$(mktemp)"
  awk '
    /^== / { repo = substr($0, 4) }
    /^   failed$/ { print "graphify-index: building the code graph of " repo " failed; check ~/.local/state/graphify-index.log, then rerun graphify-index " repo }
  ' "$out" > "$tasks"
  [[ -s "$tasks" ]] || echo "graphify-index failed (exit $code) before indexing; check ~/.local/state/graphify-index.log" > "$tasks"
  note="$(followup_note "$CLAUDE_TICKETS_VAULT" graphify-index "graphify-index" "$tasks")"
  rm -f "$tasks"
  [[ -z "$note" ]] || echo "graphify-index: follow-ups in ${note#"$CLAUDE_TICKETS_VAULT"/}"
fi
exit "$code"
