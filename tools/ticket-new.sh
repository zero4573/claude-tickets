usage() {
  cat <<'EOF'
Usage: ticket-new [--type dev|bug|investigation|chore|epic] [--priority <p>]
                  [--open] "<summary>"

Creates a manual ticket in the current vault (vault-default): one you write
and manage in Obsidian, never synced from a remote source. It takes the
next free ID (<prefix>-<n>, prefix from the `manual` source's idPrefix in
tickets/.sources.json, MAN by default), creates tickets/<ID>/<ID>.md from
templates/ticket-manual.md, and prints its path. Fill in its Description
and Acceptance criteria, then run ticket-start <ID>: manual tickets go
through the same agent workflow as synced ones.

  --type      ticket-type, which picks the agents' pipeline (default: dev;
              epic makes a lead ticket that covers others)
  --priority  free text, e.g. high
  --open      open the new note in Obsidian
EOF
}

type=dev priority="" open=0 summary=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --type) type="$(need_val "$@")"; shift 2 ;;
    --priority) priority="$(need_val "$@")"; shift 2 ;;
    --open) open=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) usage >&2; exit 1 ;;
    *) [[ -z "$summary" ]] || die "give the summary as one quoted argument"; summary="$1"; shift ;;
  esac
done
[[ -n "$summary" ]] || { usage >&2; exit 1; }
case "$type" in
  dev|bug|investigation|chore|epic) ;;
  *) die "--type must be dev, bug, investigation, chore or epic" ;;
esac

require_vault
template="$vault/templates/ticket-manual.md"
[[ -f "$template" ]] || die "no template at $template"

prefix=MAN
config="$vault/tickets/.sources.json"
if [[ -f "$config" ]]; then
  prefix="$(jq -r '.sources.manual.idPrefix // "MAN"' "$config")"
  if jq -e '.sources.manual.enabled == false' "$config" >/dev/null; then
    die "the manual source is disabled in $config"
  fi
fi
valid_key "$prefix-1" || die "manual idPrefix '$prefix' doesn't make valid ticket IDs"

# Next free number; mkdir is atomic, so two concurrent runs can't take the
# same ID
mkdir -p "$vault/tickets"
n=0
for d in "$vault/tickets/$prefix"-*/; do
  num="${d%/}"
  num="${num##*/"$prefix"-}"
  [[ "$num" =~ ^[0-9]+$ ]] && [[ "$num" -gt "$n" ]] && n="$num"
done
while true; do
  n=$((n + 1))
  id="$prefix-$n"
  mkdir "$vault/tickets/$id" 2>/dev/null && break
done

note="$vault/tickets/$id/$id.md"
today="$(date +%F)"
# YAML double-quoted scalar
yaml_summary="\"$(sed 's/\\/\\\\/g; s/"/\\"/g' <<< "$summary")\""
yaml_priority=""
[[ -n "$priority" ]] && yaml_priority="\"$(sed 's/\\/\\\\/g; s/"/\\"/g' <<< "$priority")\""
# Values go through the environment: awk -v would interpret backslashes
T_ID="$id" T_TODAY="$today" T_SUMMARY="$yaml_summary" T_TYPE="$type" \
T_PRIORITY="$yaml_priority" T_BODY_SUMMARY="$summary" awk '
  BEGIN {
    id = ENVIRON["T_ID"]; today = ENVIRON["T_TODAY"]; summary = ENVIRON["T_SUMMARY"]
    type = ENVIRON["T_TYPE"]; priority = ENVIRON["T_PRIORITY"]; body_summary = ENVIRON["T_BODY_SUMMARY"]
  }
  { gsub(/\{\{title\}\}/, id); gsub(/\{\{date\}\}/, today) }
  /^summary:/ && !done_summary { print "summary: " summary; done_summary = 1; next }
  /^ticket-type:/ && !done_type { print "ticket-type: " type; done_type = 1; next }
  /^priority:/ && !done_priority { print "priority: " priority; done_priority = 1; next }
  /^# / && !done_heading { print "# " id ": " body_summary; done_heading = 1; next }
  { print }
' "$template" > "$note"

echo "$note"
if [[ "$open" == 1 ]]; then
  xdg-open "obsidian://open?path=$(jq -rn --arg p "$note" '$p | @uri')" >/dev/null 2>&1 \
    || warn "couldn't open Obsidian; open $note yourself"
fi
