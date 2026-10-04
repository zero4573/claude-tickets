usage() {
  cat <<'EOF2'
Usage: ticket-open <ID>
       ticket-open --list

Opens a ticket's workspace (~/work/<vault>/<ID>, from ticket-start) of the
current vault (vault-default) in your editor: $CLAUDE_TICKETS_EDITOR, a command
with any arguments (default: code, VS Code).

For VS Code and its forks (code, codium, cursor, ...), it writes and opens
<ID>.code-workspace in the workspace: a multi-root workspace with one
folder per worktree (named <slug> (<branch>)) plus the ticket's notes in
the vault, so each repo gets its own source control. Rewritten on every
run, from workspace.json. Other editors get the workspace folder.

  --list   print the tickets that have a workspace: <ID> <status>
           <summary>, tab-separated (what completion uses)
EOF2
}

key="" list=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --list) list=1; shift ;;
    -*) usage >&2; exit 1 ;;
    *) [[ -z "$key" ]] || die "give one ticket ID"; key="$1"; shift ;;
  esac
done
require_vault

if [[ "$list" == 1 ]]; then
  list_tickets "$vault" --all | while IFS=$'\t' read -r id rest; do
    [[ -f "$work_root/$id/workspace.json" ]] && printf '%s\t%s\n' "$id" "$rest"
  done
  exit 0
fi
[[ -n "$key" ]] || { usage >&2; exit 1; }
valid_key "$key" || die "not a ticket ID: '$key'"
dir="$work_root/$key"
[[ -f "$dir/workspace.json" ]] \
  || die "$key has no workspace in the $(basename "$vault") vault ($dir): start it with ticket-start $key"

read -ra editor <<< "${CLAUDE_TICKETS_EDITOR:-code}"
[[ ${#editor[@]} -gt 0 ]] || die "CLAUDE_TICKETS_EDITOR is empty"
command -v "${editor[0]}" >/dev/null || die "editor '${editor[0]}' not found (set CLAUDE_TICKETS_EDITOR)"

target="$dir"
case "$(basename "${editor[0]}")" in
  code|code-insiders|codium|vscodium|cursor|windsurf)
    target="$dir/$key.code-workspace"
    # shellcheck disable=SC2016 # a jq filter
    jq --arg notes "$vault/tickets/$key" --arg key "$key" '{
      folders: (
        [.repos[] | {name: "\(.slug) (\(.branch))", path: .path}]
        + [{name: "\($key) notes (vault)", path: $notes}]
      )
    }' "$dir/workspace.json" > "$target.tmp"
    mv "$target.tmp" "$target"
    ;;
esac
exec "${editor[@]}" "$target"
