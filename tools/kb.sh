usage() {
  cat <<'EOF'
Usage: kb [--continue] [--print] [--no-fetch] ["<question>"]

Ask about the system the current vault (vault-default) describes (repos,
services, architecture, data flows, past tickets) without creating a
ticket. It runs a Claude session (the `kb` skill) with:
  * the vault, read-write, as the knowledge base
  * every repo under ~/Projects, read-only, freshly fetched on the host;
    the session explores branches in its own clones (kb-repo), where it may
    build and test, but never commits or pushes
  * ticket-new, to turn issues it finds into manual tickets
  * one code graph (graphify) merged from all the main clones
  * the MCP servers your claude command provides (e.g. Jira / Bitbucket)

Unknowns get investigated. What's learned is drafted into the vault's inbox/,
and /tickets:save in the session promotes it into projects/, knowledge-base/ and
references/.

  --continue      continue the last kb conversation for this vault
  --no-fetch      skip fetching every main clone from its remote first
  --print         answer one question and exit (claude -p), no session;
                  progress is shown live and logged to
                  ~/.local/state/kb-<vault>.log
EOF
}

cont=0 print=0 fetch=1 question=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --continue|-c) cont=1; shift ;;
    --print|-p) print=1; shift ;;
    --no-fetch) fetch=0; shift ;;
    -h|--help) usage; exit 0 ;;
    --) shift; question="$*"; break ;;
    -*) usage >&2; exit 1 ;;
    *) question="${question:+$question }$1"; shift ;;
  esac
done
[[ "$print" == 0 || -n "$question" ]] || die "--print needs a question"

require_vault
# A session may have no git credentials (e.g. sandboxed), so bring every main clone's remote
# branches up to date here; kb-repo fetch passes them on to the session
[[ "$fetch" == 1 ]] && ticket-ws fetch
name="$(basename "$vault")"

# A workspace like a ticket's, but with no repos of its own: its graph
# (ticket-graph mcp) then merges every main clone (hidden, so ticket-status
# and ticket-ws ls skip it)
dir="$work_root/.kb-$name"
ensure_workspace "$dir" KB "$vault"
trust_workspace "$dir"
# Identifies this session's inbox/ drafts for /tickets:save
session_id="kb-$name-$(date +%Y%m%d-%H%M)"

# The exploration clones (in this directory) are the session's own, so it
# may edit, build and test in them. The main clones stay untouched, and
# nothing is committed or pushed. Rule paths: // marks an absolute path.
write_session_settings "$dir" "Bash(git commit:*)" "Bash(git push:*)"

cat > "$dir/CLAUDE.md" <<EOF
# Knowledge-base session

Written by kb; regenerated on every start, so don't edit it.

- Vault (knowledge base, read-write): \`$vault\`. Read its \`AGENTS.md\`.
- Repos: main clones at \`$projects_root/<provider>/<owner>/<repo>\`, read-only,
  fetched from their remotes when this session started. \`ticket-ws repos\`
  lists every repo with its slug.
- **Exploring branches:** \`kb-repo checkout <repo> [<branch>|<tag>|<commit>]\`
  gives you an exploration clone at \`$dir/<slug>\` on that ref. The code graph
  swaps it in for the main clone and rebuilds it as the checkout changes.
  \`kb-repo fetch\` refreshes remote branches, \`kb-repo graph <repo>\` forces a
  rebuild, \`kb-repo ls\` shows what's checked out, \`kb-repo reset\` drops
  clones.
- **Experiments are fine, changes aren't kept:** in your exploration clones
  you may edit, build, run tests and add debug output to check how something
  behaves. Those edits stay in this session: never commit or push (both
  denied), and \`kb-repo checkout --force\` discards them. The main clones in
  \`~/Projects\` are never edited.
- **Issues become tickets:** when you find something that should be
  investigated or fixed, create a manual ticket with
  \`ticket-new "<summary>"\` and fill it in (see the \`kb\`
  skill), so the work goes through the ticket workflow.
- Code graph: the \`graphify\` MCP server holds every repo, with node ids
  prefixed by the repo's slug (\`bitbucket-acme-billing-service::…\`).
- Follow the \`kb\` skill: answer from the vault, then the graph, then the
  code. Investigate unknowns, draft what you learn into \`$vault/inbox/\`,
  and run \`/tickets:save\` before the session ends to promote the drafts.
- Session id (put it in the \`session:\` field of your inbox/ drafts):
  \`$session_id\`
EOF

session=()
claude_session_cmd session "$vault" "$projects_root" "$cache_dir/graphify"
if ticket-graph build >/dev/null; then
  jq -n --arg cmd "$(command -v ticket-graph)" --arg ws "$dir" --arg vault "$vault" \
    '{mcpServers: {graphify: {type: "stdio", command: $cmd, args: ["mcp", $ws], env: {CLAUDE_TICKETS_VAULT: $vault}}}}' \
    > "$dir/.claude/graph-mcp.json"
  session+=(--mcp-config "$dir/.claude/graph-mcp.json")
else
  warn "the graphify image couldn't be built; starting without the code graph"
fi
session+=(--permission-mode auto)
[[ "$cont" == 1 ]] && session+=(--continue)
if [[ "$print" == 1 ]]; then
  session+=(-p "/tickets:kb $question" --output-format stream-json --verbose)
elif [[ -n "$question" ]]; then
  session+=("/tickets:kb $question")
fi

cd "$dir"
if [[ "$print" == 0 ]]; then
  exec "${session[@]}"
fi
# One-shot answer: progress as it happens, also logged
log="$(state_log "kb-$name")"
echo "=== kb $(date -Iseconds) vault=$vault: $question" >> "$log"
run_headless "$log" "${session[@]}"
