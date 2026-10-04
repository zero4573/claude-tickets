usage() {
  cat <<'EOF2'
Usage: ticket-start [--force] [--no-fetch] [--feedback] [--no-attach] <ID>...
       ticket-start --list [--all]

Starts (or re-opens) one sandboxed Claude session for each ticket named,
from the current vault (vault-default), each in its own window of the
vault's tmux session (tickets-<vault>), running /tickets:work-ticket <ID> in the
workspace ~/work/<vault>/<ID> (the vault's .workflow.json says where).
Name one ticket or several; nothing starts without one. Each must be a
ticket of the vault: tickets/<ID>/<ID>.md, synced by ticket-sync or a
manual ticket from ticket-new. If any isn't, none start. Tab completes the
vault's open tickets (zsh).

  --force         start tickets marked `ignore: true` too, or one covered by
                  an open lead ticket (covered-by) on its own
  --no-fetch      skip fetching the main clones under ~/Projects first
  --feedback      run /tickets:pr-feedback <ID> instead of /tickets:work-ticket <ID>: apply the
                  review feedback on your open PRs for the ticket (ticket-feedback
                  is a shorthand). A ticket whose window is already open gets
                  /tickets:pr-feedback typed into it.
  --no-attach     with a single ID, don't switch to its window (by
                  default ticket-start attaches when run in a terminal)
  --list          print the vault's open tickets, one per line:
                  <ID> <status> <summary>, tab-separated (what completion uses)
  --all           with --list: done and closed tickets too

A workspace that already ran a session continues its last conversation.
The workspace is marked as trusted in Claude Code, so the session starts
without asking.
Attach with ticket-attach <ID>, see all of them with ticket-status.
EOF2
}

force=0
fetch=1
skill=work-ticket
list=0
list_all=""
attach=1
keys=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) force=1; shift ;;
    --no-fetch) fetch=0; shift ;;
    --feedback) skill=pr-feedback; shift ;;
    --list) list=1; shift ;;
    --no-attach) attach=0; shift ;;
    --all) list_all=--all; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) usage >&2; exit 1 ;;
    *) [[ " ${keys[*]} " == *" $1 "* ]] || keys+=("$1"); shift ;;
  esac
done

require_vault

if [[ "$list" == 1 ]]; then
  list_tickets "$vault" "$list_all"
  exit 0
fi
[[ ${#keys[@]} -gt 0 ]] || { usage >&2; exit 1; }

# Every ID must be one of the vault's tickets, or nothing starts
missing=()
for key in "${keys[@]}"; do
  valid_key "$key" || die "not a ticket ID: '$key'"
  [[ -f "$(ticket_note "$vault" "$key")" ]] || missing+=("$key")
done
if [[ ${#missing[@]} -gt 0 ]]; then
  die "not a ticket of the $(basename "$vault") vault: ${missing[*]} (no tickets/<ID>/<ID>.md; ticket-sync, or ticket-new for a manual ticket; ticket-start --list shows them). Nothing started."
fi

# Keys to launch, after the ignore and lead checks
launch=()
for key in "${keys[@]}"; do
  note="$(ticket_note "$vault" "$key")"
  if [[ "$force" == 0 ]] && ticket_ignored "$note"; then
    warn "$key: marked ignore: true ($(frontmatter_get "$note" ignore-reason)), skipping (use --force)"
    continue
  fi
  # A ticket covered by a lead (e.g. an epic's child) is worked in the lead's
  # workspace, on its branch
  lead="$(frontmatter_get "$note" covered-by | tr -d '[]"')"
  if [[ -n "$lead" && "$force" == 0 ]]; then
    lead_status="$(frontmatter_get "$(ticket_note "$vault" "$lead")" status)"
    if [[ "$lead_status" != "done" && "$lead_status" != "closed" ]]; then
      if [[ "$skill" == pr-feedback ]]; then
        warn "$key: covered by $lead, so its PRs are handled there: ticket-feedback $lead (or --force)"
      else
        warn "$key: covered by $lead, so it's worked there: ticket-start $lead (or --force to start it on its own)"
      fi
      continue
    fi
  fi
  if [[ "$(frontmatter_get "$note" blocked)" == true ]]; then
    warn "$key: blocked by $(frontmatter_get "$note" blocked-by | tr -d '[]"'); the kickoff will ask how to go ahead"
  fi
  launch+=("$key")
done
[[ ${#launch[@]} -gt 0 ]] || exit 1

[[ "$fetch" == 1 ]] && ticket-ws fetch

# What a session works in, given to claude as --add-dir: the vault, the main
# clones (read-only to sessions: write_session_settings), each main clone's
# .git (writable, so ticket-ws add can create worktrees and branches) and the
# graph image cache (read-only)
git_dirs=()
while IFS= read -r repo; do
  git_dirs+=("$projects_root/$repo/.git")
done < <(list_main_clones)

# The merged code graph of each workspace (ticket-graph mcp) needs its image
graph_ok=1
if ! ticket-graph build >/dev/null; then
  warn "the graphify image couldn't be built; sessions start without the merged code graph"
  graph_ok=0
fi
graph_cmd="$(command -v ticket-graph)"

for key in "${launch[@]}"; do
  dir="$work_root/$key"
  resume=0
  [[ -f "$dir/.agent-state" ]] && resume=1
  ensure_workspace "$dir" "$key" "$vault"
  write_session_settings "$dir"
  trust_workspace "$dir"

  cat > "$dir/CLAUDE.md" <<EOF
# Ticket workspace: $key

Written by ticket-start; regenerated on every start, so don't edit it.

- Ticket: \`$key\` (source: \`$(frontmatter_get "$(ticket_note "$vault" "$key")" source)\`)
- Vault: \`$vault\` (read its \`AGENTS.md\` for note rules)
- Ticket note: \`$(ticket_note "$vault" "$key")\`
- Ticket folder (the only place in the vault this session writes to before
  \`/tickets:save\`): \`$vault/tickets/$key/\`. A lead ticket (\`ticket-type: epic\`)
  may also write the work sections and \`status\` of the tickets in its
  \`covers\`.
- Repos in this workspace: \`$dir/workspace.json\` (each repo's path, base
  and branch), kept current by \`ticket-ws add\`. Checkouts are at
  \`$dir/<slug>\`, normally on \`feature/${key}[-<description>]\`.
- Main clones: \`$projects_root/<provider>/<owner>/<repo>\`. Never edit
  them: their \`.git\` is only writable so \`ticket-ws add\` can create
  worktrees. Each
  repo's slug is \`<provider>-<owner>-<repo>\` (\`ticket-ws repos\` lists
  them); the vault and the code graph name repos by slug.
- Vault tools: \`vault-lock\` and \`vault-links\` (used by \`/tickets:save\`).
  \`ticket-new\` files a manual ticket.
- Code graph: the \`graphify\` MCP server merges this workspace's worktrees
  with every other repo's main clone. Query it before reading files.

- Git: commit your work on the ticket branch in coherent steps (subject
  starting with \`$key\`, unless the repo's own convention says otherwise).
  Commits are unsigned here; the user signs them on the host with
  \`ticket-ws sign $key\`. Never push, rewrite pushed commits, reset
  --hard, or remove worktrees.

Run \`/tickets:work-ticket $key\` to (re)start the workflow, and
\`/tickets:save\` once the user has reviewed the changes. The user signs and pushes.
EOF

  prompt="/tickets:$skill $key"
  session=()
  claude_session_cmd session "$vault" "$projects_root" "${git_dirs[@]}" "$cache_dir/graphify"
  if [[ "$graph_ok" == 1 ]]; then
    jq -n --arg cmd "$graph_cmd" --arg ws "$dir" --arg vault "$vault" \
      '{mcpServers: {graphify: {type: "stdio", command: $cmd, args: ["mcp", $ws], env: {CLAUDE_TICKETS_VAULT: $vault}}}}' \
      > "$dir/.claude/graph-mcp.json"
    session+=(--mcp-config "$dir/.claude/graph-mcp.json")
  fi
  session+=(--permission-mode auto)
  [[ "$resume" == 1 ]] && session+=(--continue)
  # The vault goes on the command line: a tmux window gets its environment
  # from the tmux server (whoever started it first), not from this command
  cmd=(env CLAUDE_TICKETS_VAULT="$vault" "${session[@]}" "$prompt")
  # Keep the window open after the session ends, so its output stays readable
  shell_cmd="$(printf '%q ' "${cmd[@]}"); echo; read -rp 'Session ended. Press Enter to close this window. '"

  if ! tmux has-session -t "=$tmux_session" 2>/dev/null; then
    # Not exported to the tmux server: shells opened there later would act on
    # this vault, whatever the default
    env -u CLAUDE_TICKETS_VAULT \
      tmux new-session -d -s "$tmux_session" -n "$key" -c "$dir" bash -c "$shell_cmd"
  elif tmux list-windows -t "=$tmux_session" -F '#W' | grep -qxF "$key"; then
    if [[ "$skill" == pr-feedback ]]; then
      # Queue it in the running session as if typed
      tmux send-keys -t "=$tmux_session:$key" "$prompt" Enter
      echo "ticket-start: $key already open, sent $prompt to its window -- ticket-attach $key"
    else
      warn "$key: already has a window in tmux session '$tmux_session', not starting another"
    fi
    continue
  else
    tmux new-window -d -t "=$tmux_session:" -n "$key" -c "$dir" bash -c "$shell_cmd"
  fi
  echo "ticket-start: $key started in $tmux_session$([[ "$resume" == 1 ]] && echo " (continuing its last session)") -- ticket-attach $key"
done

# One ticket from a terminal: go straight to its window
if [[ "$attach" == 1 && ${#keys[@]} -eq 1 && -t 0 && -t 1 ]] \
   && tmux list-windows -t "=$tmux_session" -F '#W' 2>/dev/null | grep -qxF "${keys[0]}"; then
  exec ticket-attach "${keys[0]}"
fi
