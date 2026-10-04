usage() {
  cat <<'EOF2'
Usage: ticket-sync [--source <name>] [--full]

Pulls your open tickets from every enabled, syncable source in the current
vault's (vault-default) tickets/.sources.json (e.g. jira) into
tickets/<ID>/<ID>.md, and keeps the local notes reconciled with the
source: new and changed tickets are written, tickets the source closed are
closed, reassigned or deleted ones are tagged unassigned, epics are
refreshed when their children change, and `blocked` follows the blockers'
status. Sources with "sync": false (e.g. manual tickets, which you write in
Obsidian) are skipped.

For jira, the command syncs by itself, talking to the source's MCP server
(its URL from $CLAUDE_TICKETS_MCP_CONFIG; no model, no tokens). It lists the open tickets with their
last-updated time, compares them with the notes' frontmatter, and looks up
the rest by key. For each ticket that moved, Jira's changelog says what
changed, and only that part of the note is rewritten: the table and
frontmatter for field changes, the description, or the recent comments.
Changes the note doesn't show (time tracking, rank, sprint, ...) only move
the timestamp. Claude (headless, $CLAUDE_TICKETS_CLAUDE, the ticket-sync skill,
with the source's "model" in .sources.json, default sonnet) is started only
for a description or comments that Jira can only give as HTML. Other
sources run the skill over everything.

Progress is shown as it happens and logged to
~/.local/state/ticket-sync-<vault>.log. Anything left for you to check
(failed sources or tickets, tickets that left you) goes into a follow-up
note in the vault's inbox/, as tasks.

  --source <name>  sync only this source
  --full           have Claude go over every open ticket (the skill's own
                   listing), e.g. after changing the note layout

Also warns when main clones under ~/Projects have no code graph, or one
more than 7 days old, and offers to run graphify-index.
EOF2
}

only_source=""
full=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --source) only_source="$(need_val "$@")"; shift 2 ;;
    --full) full=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 1 ;;
  esac
done

require_vault
config="$vault/tickets/.sources.json"
state_file="$vault/tickets/.sync-state.json"
[[ -f "$config" ]] || die "no $config (it lists the ticket sources; see the vault's AGENTS.md)"
jq -e '.sources | type == "object"' "$config" >/dev/null 2>&1 || die "$config has no \"sources\" object"

if [[ -n "$only_source" ]]; then
  jq -e --arg s "$only_source" '.sources[$s]' "$config" >/dev/null || die "no source '$only_source' in $config"
  if jq -e --arg s "$only_source" '.sources[$s].sync == false' "$config" >/dev/null; then
    echo "ticket-sync: '$only_source' has \"sync\": false (its tickets are managed in the vault); nothing to sync"
    exit 0
  fi
fi

# Enabled, syncable sources: name<TAB>mcp server
mapfile -t sources < <(jq -r --arg only "$only_source" '
  .sources | to_entries[]
  | select(.value.enabled != false and .value.sync != false)
  | select($only == "" or .key == $only)
  | [.key, (.value.mcp // "")] | @tsv' "$config")
[[ ${#sources[@]} -gt 0 ]] || { echo "ticket-sync: no enabled sources to sync in $config"; exit 0; }

# Code graphs used by ticket sessions (see graphify-index)
stale=0
while IFS= read -r repo; do
  graph="$projects_root/$repo/graphify-out/graph.json"
  if [[ ! -f "$graph" ]] || [[ -n "$(find "$graph" -mtime +7 2>/dev/null)" ]]; then
    stale=$((stale + 1))
  fi
done < <(list_main_clones)
if [[ "$stale" -gt 0 ]]; then
  warn "$stale main clone(s) under $projects_root have a missing or week-old code graph"
  if [[ -t 0 ]]; then
    read -rp "Run graphify-index now? [y/N] " answer
    if [[ "$answer" == [yY]* ]]; then
      graphify-index || warn "graphify-index failed; syncing anyway"
    fi
  else
    warn "run graphify-index to refresh them"
  fi
fi

log="$(state_log "ticket-sync-$(basename "$vault")")"
work="$(mktemp -d)"
summary="$work/summary"
followups="$work/followups"
touch "$summary" "$followups"
cleanup() {
  rm -rf "$work" "$vault/tickets/.sync-plan.json"
}
trap cleanup EXIT

# One line of output, also kept for the notification and the log
say() {
  echo "$*" | tee -a "$summary"
}
# A task for the follow-up note
followup() {
  echo "$*" >> "$followups"
}

cd "$vault"
failed=()
for entry in "${sources[@]}"; do
  src="${entry%%$'\t'*}"
  mcp="${entry#*$'\t'}"
  if [[ -z "$mcp" ]]; then
    warn "$src: no \"mcp\" server set in $config, skipping"
    followup "ticket-sync $src: set the \"mcp\" server for the $src source in tickets/.sources.json"
    failed+=("$src")
    continue
  fi
  model="$(jq -r --arg s "$src" '.sources[$s].model // "sonnet"' "$config")"
  echo "=== ticket-sync $(date -Iseconds) vault=$vault source=$src" | tee -a "$log"

  prompt="/tickets:ticket-sync $src --followups tickets/.sync-followups"
  planned=0
  run_claude=1
  if [[ "$src" == jira && "$full" == 0 ]]; then
    if ! mcp_connect "$mcp" || ! jira_plan "$src"; then
      say "$src: couldn't reach Jira through the '$mcp' MCP server (see the messages above)"
      followup "ticket-sync $src: the run failed before syncing; check ~/.local/state/$(basename "$log")"
      failed+=("$src")
      continue
    fi
    planned=1
    run_claude=0
    jq -r '"\(.source): open \(.counts.open), new \(.create | length), to refresh \(.refresh | length), to close \(.close | length), gone \(.gone | length), blocked flips \(.blocked | length), unchanged \(.counts.skipped)"' \
      "$work/plan.json" | tee -a "$summary"
    # Each ticket's changes are found and written here; failures become
    # follow-ups and are retried next run (the note's source-updated stays)
    jira_apply_plan || true
    # Only what Jira can give as HTML alone goes to Claude, part by part
    if [[ -s "$work/html.jsonl" ]]; then
      jq -n --slurpfile h "$work/html.jsonl" --slurpfile p "$work/plan.json" \
        '{source: $p[0].source, cloudId: $p[0].cloudId, accountId: $p[0].accountId, html: $h}' \
        > "$vault/tickets/.sync-plan.json"
      prompt="/tickets:ticket-sync $src --plan tickets/.sync-plan.json --followups tickets/.sync-followups"
      run_claude=1
      echo "ticket-sync: $src: $(wc -l < "$work/html.jsonl") HTML-only part(s) go to Claude" | tee -a "$log"
    else
      echo "ticket-sync: $src: written without Claude" | tee -a "$log"
    fi
  fi

  if [[ "$run_claude" == 1 ]]; then
    rm -f "$vault/tickets/.sync-followups"
    # --strict-mcp-config: only the MCP servers given on the command line (by
    # $CLAUDE_TICKETS_CLAUDE, or whatever wraps claude), not the claude.ai
    # connectors tied to the login, which need interactive auth.
    # $CLAUDE_TICKETS_SESSION_SETTINGS carries your own rules (e.g. which MCP
    # tools a sync may never call).
    claude_session_cmd sync_cmd
    [[ -z "${CLAUDE_TICKETS_SESSION_SETTINGS:-}" ]] || sync_cmd+=(--settings "$CLAUDE_TICKETS_SESSION_SETTINGS")
    if ! run_headless "$summary" "${sync_cmd[@]}" \
         -p "$prompt" \
         --model "$model" \
         --strict-mcp-config \
         --permission-mode dontAsk \
         --allowedTools "mcp__$mcp,Read,Write,Edit,Glob,Grep,Bash(date:*)" \
         --output-format stream-json --verbose; then
      failed+=("$src")
      followup "ticket-sync $src: Claude's run failed; check ~/.local/state/$(basename "$log")"
    fi
    if [[ -s "$vault/tickets/.sync-followups" ]]; then
      cat "$vault/tickets/.sync-followups" >> "$followups"
    fi
    rm -f "$vault/tickets/.sync-followups" "$vault/tickets/.sync-plan.json"
  fi

  # In plan mode this command keeps the sync state (the skill keeps it otherwise)
  if [[ "$planned" == 1 && " ${failed[*]} " != *" $src "* ]]; then
    [[ -s "$state_file" ]] || echo '{"sources": {}}' > "$state_file"
    # Children baselines: the planner's, plus epics created in this run
    jq -sc 'group_by(.epic) | map({key: .[0].epic, value: (map(.sig) | sort)}) | from_entries' \
      "$work/children.jsonl" > "$work/children-after.json"
    # shellcheck disable=SC2016 # a jq filter
    json_update "$state_file" --arg s "$src" --arg now "$(date -Iseconds)" \
      --slurpfile p "$work/plan.json" --slurpfile c "$work/children-after.json" '
      .sources[$s] = {lastSync: $now, open: $p[0].counts.open, skipped: $p[0].counts.skipped,
        created: $p[0].create, updated: ($p[0].refresh | map(.id)), closed: $p[0].close,
        gone: $p[0].gone, blocked: ($p[0].blocked | map(.id)),
        epicChildren: ($p[0].epicChildren + $c[0])}'
  fi
done
cat "$summary" >> "$log"

note=""
if [[ -s "$followups" ]]; then
  note="$(followup_note "$vault" ticket-sync "ticket-sync" "$followups" tickets.base)"
  echo "ticket-sync: follow-ups for you in ${note#"$vault"/}" | tee -a "$log"
fi

if [[ ${#failed[@]} -eq 0 ]]; then
  notify-send -a "ticket-sync" "ticket-sync done" "$(tail -n 8 "$summary")${note:+$'\n'Follow-ups: ${note##*/}}" || true
else
  notify-send -u critical -a "ticket-sync" "ticket-sync: ${failed[*]} failed" "See $log${note:+ and ${note##*/}}" || true
  exit 1
fi
