usage() {
  cat <<'EOF2'
Usage: ticket-status

One row per ticket workspace of the current vault (vault-default), tickets
waiting on you first:
  AGENT   what the session is doing, from hooks (.agent-state):
          needs-input (a question or permission prompt is waiting),
          idle (finished its turn, waiting for your next message),
          working, exited; "stale" when its pod is no longer running
  WINDOW  whether its window in the vault's tmux session is open
  SOURCE  where the ticket comes from (jira, manual, ...)
  STATUS  the ticket note's status in the vault
  REPOS   worktrees, and how many have uncommitted changes
EOF2
}
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  "") ;;
  *) usage >&2; exit 1 ;;
esac
require_vault

windows=""
windows="$(tmux list-windows -t "=$tmux_session" -F '#W' 2>/dev/null || true)"

rows=()
for ws in "$work_root"/*/workspace.json; do
  [[ -f "$ws" ]] || continue
  dir="$(dirname "$ws")"
  key="$(basename "$dir")"

  state="-" reason="" since=""
  if [[ -f "$dir/.agent-state" ]]; then
    state="$(jq -r '.state // "-"' "$dir/.agent-state")"
    reason="$(jq -r '.reason // ""' "$dir/.agent-state")"
    since="$(jq -r '.ts // ""' "$dir/.agent-state")"
  fi
  if [[ "$state" != exited && "$state" != - ]] && ! session_running "$dir"; then
    state="$state (stale)"
  fi

  window=no
  grep -qxF "$key" <<< "$windows" && window=yes

  vault_status="-" source="-"
  vault="$(jq -r '.vault // empty' "$ws")"
  if [[ -z "$vault" ]]; then
    # Workspaces from before workspace.json recorded the vault
    # shellcheck disable=SC2016 # literal backticks
    vault="$(sed -n 's/^- Vault: `\([^`]*\)`.*/\1/p' "$dir/CLAUDE.md" 2>/dev/null | head -n1)"
  fi
  if [[ -n "$vault" ]]; then
    vault_status="$(frontmatter_get "$(ticket_note "$vault" "$key")" status)"
    [[ -n "$vault_status" ]] || vault_status="-"
    source="$(frontmatter_get "$(ticket_note "$vault" "$key")" source)"
    [[ -n "$source" ]] || source="-"
  fi

  total=0 dirty=0
  while IFS= read -r path; do
    [[ -e "$path/.git" ]] || continue
    total=$((total + 1))
    [[ -n "$(git -C "$path" status --porcelain 2>/dev/null)" ]] && dirty=$((dirty + 1))
  done < <(jq -r '.repos[].path' "$ws")

  case "$state" in
    needs-input*) order=0 ;;
    idle*) order=1 ;;
    working*) order=2 ;;
    *) order=3 ;;
  esac
  [[ ${#reason} -gt 60 ]] && reason="${reason:0:57}..."
  rows+=("$order"$'\t'"$key"$'\t'"$source"$'\t'"$state"$'\t'"$window"$'\t'"$vault_status"$'\t'"$total ($dirty dirty)"$'\t'"${since:11:8}"$'\t'"$reason")
done

# Other vaults' sessions run in their own tickets-<vault> session (vault-default
# to see them)
echo "Vault $(basename "$vault"), tmux session $tmux_session"

if [[ ${#rows[@]} -eq 0 ]]; then
  echo "No ticket workspaces under $work_root (start one with ticket-start <ID>)."
  exit 0
fi

{
  printf 'TICKET\tSOURCE\tAGENT\tWINDOW\tSTATUS\tREPOS\tSINCE\tWAITING ON\n'
  printf '%s\n' "${rows[@]}" | sort -t $'\t' -k1,1n -k2,2 | cut -f2-
} | column -t -s $'\t'
