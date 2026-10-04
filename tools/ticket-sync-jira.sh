# --- Jira sync without a model ---
# Two stages, both through the MCP client (ticket-sync-mcp.sh):
#
# jira_plan: a full reconciliation of the local notes against Jira, from
# cheap listings (key, updated, status):
#   create      open in Jira, no note
#   refresh     `updated` differs from the note's source-updated (or the
#               note predates the dependency fields); an epic whose children
#               changed (reason children); a parent epic or a ticket that left
#               you, when it changed
#   close       local note open, Jira status category done
#   gone        open note whose key Jira no longer returns (deleted, moved,
#               or no access): tagged unassigned, plus a follow-up task
#   blocked     recomputed from the blockers' status categories
#
# jira_apply: per ticket, finds what changed and patches only that. The
# changelog since the note's source-updated says which fields moved:
#   time tracking, rank, sprint, ... (nothing the note shows)  timestamp only
#   status, assignee, summary, versions, links, parent, ...   frontmatter and
#                                                              the block's table
#   description (or a text custom field)                       ### Description
# and the last comments' ids/timestamps, against the signature kept in the
# note, decide ### Recent comments. New tickets get every part. A
# description or comment that Jira can only give as HTML (panels, mentions,
# media) is handed to Claude (the ticket-sync skill, plan mode) for just that
# part; everything else is written here, deterministically.
jira_cloud=""
jira_me=""
jira_url=""

# Changelog fields the note shows (anything else only moves the timestamp)
JIRA_TABLE_FIELDS='["summary", "issuetype", "status", "resolution", "priority", "assignee", "reporter", "Fix Version", "Component", "labels", "Parent", "IssueParentAssociation", "Epic Link", "Link", "Key"]'
# Text custom fields shown under ### Description, by label (the source's
# "textFields" in .sources.json overrides it)
JIRA_TEXT_FIELDS='["QA Testing Instructions", "Acceptance Criteria", "Steps to Reproduce", "Expected Result", "Actual Result"]'
# Fields fetched for the table and frontmatter
JIRA_TABLE_FETCH='["summary", "issuetype", "status", "priority", "assignee", "reporter", "fixVersions", "components", "labels", "parent", "issuelinks", "subtasks", "updated"]'

# Shared jq definitions for rendering
# shellcheck disable=SC2016 # jq definitions
JQ_DEFS='
def cell: tostring | gsub("\n"; " ") | gsub("\\|"; "\\|");
def yamlstr: tostring | if test("^[^\\[\\]{}#&*!|>%@`\"\u0027,?:-][^#\"]*$") and (test(": ") | not) and (test(":$") | not) then . else tojson end;
def link: "\"[[\(.)]]\"";
def links: map(link) | "[" + join(", ") + "]";
def category: {"new": "todo", "indeterminate": "in-progress", "done": "done"}[.] // "todo";
def tickettype: .issuetype as $t
  | if ($t.name // "") == "Epic" or ($t.hierarchyLevel // 0) >= 1 then "epic"
    elif $t.name == "Bug" then "bug"
    elif ($t.name // "" | test("^(Spike|Investigation|Research)$")) then "investigation"
    elif ($t.name // "" | test("^(Story|Task|Improvement|New Feature|Sub-task|Subtask)$")) then "dev"
    else "chore" end;
'

# jira_search <jql> <fields json>: every matching issue, one JSON per line
jira_search() {
  local jql="$1" fields="$2" token="" page args
  while true; do
    args="$(jq -nc --arg c "$jira_cloud" --arg q "$jql" --argjson f "$fields" --arg t "$token" \
      '{cloudId: $c, jql: $q, fields: $f, maxResults: 100, view: "full"} + (if $t == "" then {} else {nextPageToken: $t} end)')"
    page="$(mcp_tool searchJiraIssuesUsingJql "$args")" || return 1
    jq -c '.data.issues[]?' <<< "$page"
    token="$(jq -r 'if .data.isLast == true then "" else .data.nextPageToken // "" end' <<< "$page")"
    [[ -n "$token" ]] || return 0
  done
}

# jira_keys <fields json> <key>...: those issues (missing keys are left out,
# as Jira does), in batches of 50
jira_keys() {
  local fields="$1" batch
  shift
  while [[ $# -gt 0 ]]; do
    batch="$(printf '%s\n' "${@:1:50}" | paste -sd, -)"
    jira_search "key in ($batch)" "$fields" || return 1
    shift $(( $# < 50 ? $# : 50 ))
  done
}

# jira_issue <key> <extra args json>: getJiraIssue's .data
jira_issue() {
  mcp_tool getJiraIssue "$(jq -nc --arg c "$jira_cloud" --arg k "$1" --argjson x "$2" \
    '{cloudId: $c, issueIdOrKey: $k} + $x')" | jq -c '.data'
}

# jira_comments <key>: the last 5 comments' page (listJiraIssueComments)
jira_comments() {
  mcp_tool executeRead "$(jq -nc --arg c "$jira_cloud" --arg k "$1" \
    '{name: "listJiraIssueComments", cloudId: $c, inputs: {issueIdOrKey: $k, maxResults: 5, orderBy: "-created"}}')" \
    | jq -c '.data'
}

# One TSV line per jira note: id, source-updated, status, ticket-type,
# tags, has blocked-by (0/1), blocked, blocked-by
local_index() {
  awk '
    function flush() {
      if (file != "" && f["source"] == "jira") {
        id = f["source-id"]
        if (id == "") { id = file; sub(/\/[^\/]*$/, "", id); sub(/.*\//, "", id) }
        print id "\t" f["source-updated"] "\t" f["status"] "\t" f["ticket-type"] "\t" f["tags"] "\t" ("blocked-by" in f ? 1 : 0) "\t" f["blocked"] "\t" f["blocked-by"]
      }
      delete f
    }
    FNR == 1 { flush(); file = FILENAME; infm = ($0 == "---"); next }
    infm && $0 == "---" { infm = 0; next }
    infm && match($0, /^[a-z][a-z-]*:/) {
      k = substr($0, 1, RLENGTH - 1); v = substr($0, RLENGTH + 1)
      sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v); gsub(/^"|"$/, "", v)
      f[k] = v
    }
    END { flush() }' "$vault"/tickets/*/*.md 2>/dev/null || true
}

# frontmatter_set <file> <key> <value>: set a frontmatter field (added at
# the end of the frontmatter when missing). The value is written as given.
frontmatter_set() {
  local file="$1" tmp="$1.sync.tmp"
  FM_KEY="$2" FM_VAL="$3" awk '
    BEGIN { k = ENVIRON["FM_KEY"]; v = ENVIRON["FM_VAL"] }
    NR == 1 && $0 == "---" { infm = 1; print; next }
    infm && $0 == "---" { if (!done) print k ":" (v == "" ? "" : " " v); infm = 0; done = 1 }
    infm && index($0, k ":") == 1 { print k ":" (v == "" ? "" : " " v); done = 1; next }
    { print }' "$file" > "$tmp" && mv "$tmp" "$file"
}

# tags_edit <file> <add|remove> <tag>: in a flow-style `tags: [...]` list
tags_edit() {
  local file="$1" tmp="$1.sync.tmp"
  OP="$2" TAG="$3" awk '
    BEGIN { op = ENVIRON["OP"]; t = ENVIRON["TAG"] }
    NR == 1 && $0 == "---" { infm = 1; print; next }
    infm && $0 == "---" { infm = 0 }
    infm && /^tags: *\[.*\] *$/ {
      s = $0; sub(/^tags: *\[/, "", s); sub(/\] *$/, "", s)
      n = split(s, a, ","); out = ""; found = 0
      for (i = 1; i <= n; i++) {
        x = a[i]; gsub(/^ +| +$/, "", x)
        if (x == "") continue
        if (x == t) { found = 1; if (op == "remove") continue }
        out = out (out == "" ? "" : ", ") x
      }
      if (op == "add" && !found) out = out (out == "" ? "" : ", ") t
      print "tags: [" out "]"; next
    }
    { print }' "$file" > "$tmp" && mv "$tmp" "$file"
}
add_tag() { tags_edit "$1" add "$2"; }
remove_tag() { tags_edit "$1" remove "$2"; }

# --- The source block, as four parts: the table (head), ### Children,
# ### Description, ### Recent comments, always written in that order.
# block_split <note> <dir>: the note's current parts into <dir>/{head,
# children,description,comments} (empty files when absent); fails when the
# note has no source block
block_split() {
  local note="$1" dir="$2"
  grep -q '^<!-- source:start -->$' "$note" && grep -q '^<!-- source:end -->$' "$note" || return 1
  : > "$dir/head"; : > "$dir/children"; : > "$dir/description"; : > "$dir/comments"
  DIR="$dir" awk '
    BEGIN { d = ENVIRON["DIR"]; part = "head" }
    $0 == "<!-- source:start -->" { inb = 1; next }
    $0 == "<!-- source:end -->" { inb = 0; next }
    !inb { next }
    $0 == "### Children" { part = "children" }
    $0 == "### Description" { part = "description" }
    $0 == "### Recent comments" { part = "comments" }
    { print > (d "/" part) }' "$note"
}

# block_write <note> <dir>: replace the note's source block with the parts in
# <dir> (adding the block after the title heading when there's none)
block_write() {
  local note="$1" dir="$2" tmp="$1.sync.tmp" part
  {
    echo '<!-- source:start -->'
    for part in head children description comments; do
      # Each part ends with exactly one blank line, except the last
      [[ -s "$dir/$part" ]] && sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' "$dir/$part" && echo
    done | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
    echo '<!-- source:end -->'
  } > "$dir/block"
  if grep -q '^<!-- source:start -->$' "$note"; then
    BLOCK="$dir/block" awk '
      $0 == "<!-- source:start -->" { while ((getline l < ENVIRON["BLOCK"]) > 0) print l; skip = 1; next }
      skip && $0 == "<!-- source:end -->" { skip = 0; next }
      !skip { print }' "$note" > "$tmp"
  else
    BLOCK="$dir/block" awk '
      { print }
      !done && /^# / { print ""; while ((getline l < ENVIRON["BLOCK"]) > 0) print l; done = 1 }' "$note" > "$tmp"
  fi
  mv "$tmp" "$note"
}

# jira_plan <source>: writes $work/plan.json; returns 1 when the source
# can't be reached (already reported)
jira_plan() {
  local src="$1" site today
  today="$(date +%F)"
  site="$(jq -r --arg s "$src" '.sources[$s].site // empty' "$config")"
  if [[ -n "$site" ]]; then
    jira_cloud="$site"
    jira_url="https://${site#https://}"
  else
    local res
    res="$(mcp_tool getAccessibleAtlassianResources '{}' \
      | jq -c '[.data.resources[]? | select(any(.products[]?; .id == "jira"))] | if length == 1 then .[0] else empty end')" || true
    if [[ -z "$res" ]]; then
      say "$src: can't pick a Jira site (none, or several, reachable): set \"site\" in tickets/.sources.json"
      followup "ticket-sync $src: set \"site\" for the $src source in tickets/.sources.json (the token reaches no single Jira site)"
      return 1
    fi
    jira_cloud="$(jq -r .cloudId <<< "$res")"
    jira_url="$(jq -r .url <<< "$res")"
  fi
  jira_url="${jira_url%/}"
  jira_me="$(mcp_tool atlassianUserInfo '{}' | jq -r '.data.accountId // .accountId // empty')" || true
  [[ -n "$jira_me" ]] || { say "$src: couldn't read your Jira account id (atlassianUserInfo)"; return 1; }

  local query
  query="$(jq -r --arg s "$src" '.sources[$s].query // empty' "$config")"
  [[ -n "$query" ]] || { say "$src: no \"query\" in tickets/.sources.json"; followup "ticket-sync $src: set \"query\" in tickets/.sources.json"; return 1; }

  echo "ticket-sync: $src: listing open tickets..." >&2
  jira_search "$query" '["updated", "status"]' \
    | jq -c '{key, updated: .fields.updated, cat: .fields.status.statusCategory.key}' > "$work/remote.jsonl" || return 1
  local_index | jq -Rc 'split("\t") | {id: .[0], updated: .[1], status: .[2], type: .[3],
      tags: (.[4] | ltrimstr("[") | rtrimstr("]") | split(",") | map(gsub("^ +| +$"; ""))),
      hasBlockedBy: (.[5] == "1"), blocked: .[6],
      blockers: ([.[7] | scan("\\[\\[([A-Z][A-Z0-9_]*-[A-Z0-9-]+)\\]\\]")[0]])}' > "$work/local.jsonl"

  # Open notes Jira's query didn't return: look them up by key
  local leavers
  mapfile -t leavers < <(jq -rn --slurpfile r "$work/remote.jsonl" --slurpfile l "$work/local.jsonl" '
    ($r | map(.key)) as $open | $l[] | select(.status != "closed" and (.id as $i | $open | index($i) | not)) | .id')
  : > "$work/leavers.jsonl"
  if [[ ${#leavers[@]} -gt 0 ]]; then
    echo "ticket-sync: $src: checking ${#leavers[@]} ticket(s) no longer in your open list..." >&2
    jira_keys '["updated", "status", "assignee"]' "${leavers[@]}" \
      | jq -c '{key, updated: .fields.updated, cat: .fields.status.statusCategory.key, assignee: (.fields.assignee.accountId // null)}' \
      > "$work/leavers.jsonl" || return 1
  fi

  # Epics' children: refresh an epic when they (who, which status) changed.
  # The rows are kept for the children table the applier writes.
  local epics
  mapfile -t epics < <(jq -r 'select(.type == "epic" and .status != "closed") | .id' "$work/local.jsonl")
  : > "$work/children.jsonl"
  if [[ ${#epics[@]} -gt 0 ]]; then
    jira_children "${epics[@]}" >> "$work/children.jsonl" || return 1
  fi
  jq -sc 'group_by(.epic) | map({key: .[0].epic, value: (map(.sig) | sort)}) | from_entries' \
    "$work/children.jsonl" > "$work/children.json"

  # Blockers' status, for `blocked` on every open note
  local blockers
  mapfile -t blockers < <(jq -r 'select(.status != "closed") | .blockers[]' "$work/local.jsonl" | sort -u)
  : > "$work/blockers.jsonl"
  if [[ ${#blockers[@]} -gt 0 ]]; then
    jira_keys '["status"]' "${blockers[@]}" \
      | jq -c '{key, cat: .fields.status.statusCategory.key}' > "$work/blockers.jsonl" || return 1
  fi

  jq -n --arg src "$src" --arg cloud "$jira_cloud" --arg me "$jira_me" \
    --slurpfile r "$work/remote.jsonl" --slurpfile l "$work/local.jsonl" \
    --slurpfile lv "$work/leavers.jsonl" --slurpfile bl "$work/blockers.jsonl" \
    --slurpfile ch "$work/children.json" \
    --argjson known "$(jq -c --arg s "$src" '.sources[$s].epicChildren // null' "$state_file" 2>/dev/null || echo null)" '
    def has($list; $x): $list | index($x) != null;
    ($l | map({key: .id, value: .}) | from_entries) as $notes
    | ($r | map({key, value: .}) | from_entries) as $open
    | ($lv | map({key, value: .}) | from_entries) as $left
    | ($bl | map({key, value: .cat}) | from_entries) as $bcat
    | ([$r[] | select($notes[.key] == null) | .key]) as $create
    | ([$r[] | . as $x | $notes[.key] | select(. != null)
        | select(.updated != $x.updated or (.hasBlockedBy | not))
        | {id, reason: (if .updated != $x.updated then "changed" else "dependencies" end)}]) as $changed
    | ([$l[] | select(.status != "closed" and $open[.id] == null)]) as $leaving
    | ([$leaving[] | select($left[.id] == null and (has(.tags; "unassigned") | not)) | .id]) as $gone
    | ([$leaving[] | select($left[.id].cat == "done") | .id]) as $close
    | ([$leaving[] | . as $n | $left[.id] | select(. != null and .cat != "done") | . as $j
        | if has($n.tags; "parent-epic") then
            select($j.updated != $n.updated) | {id: $n.id, reason: "parent-epic"}
          elif $j.assignee != $me then
            select($j.updated != $n.updated or (has($n.tags; "unassigned") | not)) | {id: $n.id, reason: "unassigned"}
          else
            select($j.updated != $n.updated) | {id: $n.id, reason: "changed"}
          end]) as $left_refresh
    | ([$l[] | select(.type == "epic" and .status != "closed") | .id]
        | map({key: ., value: ($ch[0][.] // [])}) | from_entries) as $children
    # No baseline yet (first planned run): the last full sync wrote the
    # children, so record them without refreshing every epic
    | (if $known == null then [] else [$children | to_entries[] | select($known[.key] != .value) | .key] end) as $epic_changed
    | ($changed + $left_refresh) as $base
    | ($base + [$epic_changed[] | select(has($base | map(.id); .) | not) | {id: ., reason: "children"}]) as $refresh
    | ($create + $close + ($refresh | map(.id))) as $agent
    | ([$l[] | select(.status != "closed" and (.blockers | length) > 0 and (has($agent; .id) | not))
        | [.blockers[] | $bcat[.] // (if (($notes[.].status // "") | test("^(done|closed)$")) then "done" else empty end)] as $cats
        | select($cats | length > 0)
        | ($cats | any(. != "done")) as $now
        | select((.blocked == "true") != $now)
        | {id, blocked: $now}]) as $blocked
    | {source: $src, cloudId: $cloud, accountId: $me,
       create: $create, refresh: $refresh, close: $close, gone: $gone, blocked: $blocked,
       epicChildren: $children,
       counts: {open: ($r | length), skipped: (($r | length) - ($create | length) - ($changed | length))}}' \
    > "$work/plan.json"

  # Written here: tags for vanished tickets, `blocked` flips
  local id file v
  while IFS= read -r id; do
    file="$(ticket_note "$vault" "$id")"
    add_tag "$file" unassigned
    frontmatter_set "$file" updated "$today"
    say "$id: no longer found in Jira (deleted, moved, or no access): tagged unassigned"
    followup "Check [[$id]]: Jira no longer returns it (deleted, moved to another project, or access lost); close or delete the note"
  done < <(jq -r '.gone[]' "$work/plan.json")
  while IFS=$'\t' read -r id v; do
    file="$(ticket_note "$vault" "$id")"
    frontmatter_set "$file" blocked "$v"
    frontmatter_set "$file" updated "$today"
    say "$id: blocked: $v (blockers' status changed)"
  done < <(jq -r '.blocked[] | [.id, (.blocked | tostring)] | @tsv' "$work/plan.json")
}

# jira_children <epic>...: one JSON line per child, every assignee:
# {epic, key, summary, type, assignee, mine, status, cat, sig}
jira_children() {
  local batch rest=("$@")
  while [[ ${#rest[@]} -gt 0 ]]; do
    batch="$(printf '%s\n' "${rest[@]:0:50}" | paste -sd, -)"
    rest=("${rest[@]:50}")
    jira_search "parent in ($batch)" '["summary", "issuetype", "status", "assignee", "parent"]' \
      | jq -c --arg me "$jira_me" '
        ((.fields.assignee.accountId // "") == $me) as $mine
        | {epic: .fields.parent.key, key, summary: .fields.summary, type: .fields.issuetype.name,
           assignee: (.fields.assignee.displayName // "Unassigned"), mine: $mine,
           status: .fields.status.name, cat: .fields.status.statusCategory.key,
           sig: "\(.key):\(.fields.status.statusCategory.key):\(if $mine then "mine" else "other" end)"}' || return 1
  done
}

# --- Applier ---

# jira_write_children <epic> <note> <dir>: the epic's children part and its
# children / covers fields, plus covered-by on the children's own notes
jira_write_children() {
  local epic="$1" note="$2" dir="$3" rows child cnote cur
  rows="$work/children-$epic.jsonl"
  jq -c --arg e "$epic" 'select(.epic == $e)' "$work/children.jsonl" > "$rows"
  if [[ ! -s "$rows" ]] && ! grep -q "\"epic\":\"$epic\"" "$work/children.jsonl" 2>/dev/null; then
    jira_children "$epic" > "$rows" || return 1
    cat "$rows" >> "$work/children.jsonl"
  fi
  jq -rs "$JQ_DEFS"'
    sort_by(.key)
    | if length == 0 then empty else
      (["### Children", "| Ticket | Summary | Type | Assignee | Status |", "|---|---|---|---|---|"]
       + map("| [[\(.key)]]\(if .mine then "" else " *(not yours)*" end) | \(.summary | cell) | \(.type | cell) | \(.assignee | cell) | \(.status | cell) |")
       | join("\n")) end' "$rows" > "$dir/children"
  frontmatter_set "$note" children "$(jq -rs "$JQ_DEFS"'map(.key) | sort | links' "$rows")"
  frontmatter_set "$note" covers "$(jq -rs "$JQ_DEFS"'map(select(.mine and .cat != "done") | .key) | sort | links' "$rows")"
  # covered-by on the covered children's notes (never over a manual lead)
  while IFS= read -r child; do
    cnote="$(ticket_note "$vault" "$child")"
    [[ -f "$cnote" && "$(frontmatter_get "$cnote" source)" == jira ]] || continue
    cur="$(frontmatter_get "$cnote" covered-by)"
    [[ "$cur" == "[[MAN-"* ]] && continue
    [[ "$cur" == "[[$epic]]" ]] || frontmatter_set "$cnote" covered-by "\"[[$epic]]\""
  done < <(jq -r 'select(.mine and .cat != "done") | .key' "$rows")
  # and off the notes that left it
  while IFS= read -r cnote; do
    child="$(basename "$cnote" .md)"
    jq -e --arg k "$child" 'select(.key == $k and .mine and .cat != "done")' "$rows" >/dev/null 2>&1 \
      || frontmatter_set "$cnote" covered-by ""
  done < <(grep -l "^covered-by: \"\[\[$epic\]\]\"" "$vault"/tickets/*/*.md 2>/dev/null || true)
}

# jira_apply <key> <create|refresh|close> <reason>: bring one note up to date.
# Prints what changed; adds to $work/html.jsonl what only Claude can write.
jira_apply() {
  local key="$1" mode="$2" reason="${3:-}" note dir today since all=0 table=0 desc=0
  local changes=() updated="" issue ch text_fields fields
  note="$(ticket_note "$vault" "$key")"
  dir="$work/apply-$key"
  mkdir -p "$dir"
  today="$(date +%F)"
  text_fields="$(jq -c --arg s "$src" --argjson d "$JIRA_TEXT_FIELDS" '.sources[$s].textFields // $d' "$config")"

  if [[ "$mode" == create ]]; then
    mkdir -p "$(dirname "$note")"
    sed -e "s/{{title}}/$key/g" -e "s/{{date}}/$today/g" "$vault/templates/ticket-synced.md" > "$note"
    : > "$dir/head"; : > "$dir/children"; : > "$dir/description"; : > "$dir/comments"
    all=1
  elif ! block_split "$note" "$dir"; then
    all=1
  fi

  # What moved since the note's last sync
  since="$(frontmatter_get "$note" source-updated)"
  if [[ "$all" == 0 && "$reason" == children ]]; then
    : # only the children part, below
  elif [[ "$all" == 0 && -n "$since" ]]; then
    ch="$(jira_issue "$key" '{"fields": ["updated"], "fieldsByKeys": true, "expand": "changelog"}')" || return 1
    updated="$(jq -r '.fields.updated // empty' <<< "$ch")"
    jq -c --arg since "$since" --argjson table "$JIRA_TABLE_FIELDS" --argjson text "$text_fields" '
      [.changelog.histories[]? | select(.created > $since) | .items[]? | .field] as $f
      | {fields: ([$f[] | . as $x | select($table + $text + ["description"] | index($x) != null)] | unique),
         table: any($f[]; . as $x | $table | index($x) != null),
         desc: any($f[]; . == "description" or (. as $x | $text | index($x) != null)),
         truncated: (((.changelog.total // 0) > (.changelog.histories | length))
                     and (((.changelog.histories | last | .created) // "") > $since))}' \
      <<< "$ch" > "$dir/delta"
    jq -e '.truncated' "$dir/delta" >/dev/null && all=1
    jq -e '.table' "$dir/delta" >/dev/null && table=1
    jq -e '.desc' "$dir/delta" >/dev/null && desc=1
    mapfile -t fields < <(jq -r '.fields[]' "$dir/delta")
    [[ ${#fields[@]} -gt 0 ]] && changes+=("$(IFS=,; echo "${fields[*]}")")
  else
    all=1
  fi
  # Reasons that need the current fields whatever the history says
  case "$reason" in unassigned|dependencies|parent-epic) table=1 ;; esac
  [[ "$mode" == close ]] && table=1
  if [[ "$all" == 1 ]]; then table=1; desc=1; fi

  # Table and frontmatter
  if [[ "$table" == 1 ]]; then
    issue="$(jira_issue "$key" "$(jq -nc --argjson f "$JIRA_TABLE_FETCH" '{fields: $f, fieldsByKeys: true, view: "full"}')")" || return 1
    updated="$(jq -r '.fields.updated // empty' <<< "$issue")"
    jq -r --arg url "$jira_url" "$JQ_DEFS"'
      .key as $k | .fields as $f
      | (($f.components // []) | map(.name) | if length == 0 then "none" else join(", ") end) as $comps
      | (($f.labels // []) | if length == 0 then "none" else join(", ") end) as $labels
      | (["## Source (Jira)",
          "> Synced from Jira by ticket-sync. Edits here are overwritten. Dashboard: [[tickets.base|Tickets]]",
          "",
          "| | |", "|---|---|",
          "| Summary | \($f.summary // "" | cell) |",
          "| Type / priority | \($f.issuetype.name // "?" | cell) / \($f.priority.name // "none" | cell) |",
          "| Status | \($f.status.name // "?" | cell) |",
          "| Assignee / reporter | \($f.assignee.displayName // "Unassigned" | cell) / \($f.reporter.displayName // "unknown" | cell) |",
          "| Fix versions | \(($f.fixVersions // []) | map(.name) | if length == 0 then "none set" else join(", ") end | cell) |",
          "| Components / labels | \($comps | cell) / \($labels | cell) |"]
         + (if $f.parent then ["| Parent | [[\($f.parent.key)]] (\($f.parent.fields.summary // "" | cell)) |"] else [] end)
         + (if ($f.subtasks // []) | length > 0 then
              ["| Subtasks | " + (($f.subtasks | map("[[\(.key)]] (\(.fields.summary // "" | cell), \(.fields.status.name // "?" | cell))")) | join("; ")) + " |"]
            else [] end)
         + (if ($f.issuelinks // []) | length > 0 then
              ["| Links | " + (($f.issuelinks | map(
                  if .inwardIssue then "\(.type.inward) [[\(.inwardIssue.key)]]" else "\(.type.outward) [[\(.outwardIssue.key)]]" end)) | join(", ") | cell) + " |"]
            else [] end)
         + ["| URL | \($url)/browse/\($k) |"])
      | join("\n")' <<< "$issue" > "$dir/head"

    # Frontmatter fields the sync owns, one TSV line each
    jq -r --arg me "$jira_me" "$JQ_DEFS"'
      .fields as $f
      | ($f.issuelinks // []) as $ln
      | [$ln[] | select(.type.name == "Blocks" and .inwardIssue) | .inwardIssue] as $by
      | [["summary", ($f.summary // "" | yamlstr)],
         ["source-type", ($f.issuetype.name // "" | yamlstr)],
         ["source-status", ($f.status.name // "" | yamlstr)],
         ["source-status-category", ($f.status.statusCategory.key // "new" | category)],
         ["source-priority", ($f.priority.name // "" | yamlstr)],
         ["source-assignee", ($f.assignee.displayName // "Unassigned" | yamlstr)],
         ["source-fix-versions", (($f.fixVersions // []) | map(.name) | tojson)],
         ["parent", (if $f.parent then ($f.parent.key | link) else "" end)],
         ["blocked-by", ($by | map(.key) | links)],
         ["blocks", ([$ln[] | select(.type.name == "Blocks" and .outwardIssue) | .outwardIssue.key] | links)],
         ["related", ([$ln[] | select(.type.name != "Blocks") | (.inwardIssue // .outwardIssue).key] | unique | links)],
         ["blocked", ([$by[] | .fields.status.statusCategory.key // "new"] | any(. != "done") | tostring)],
         ["_mine", (($f.assignee.accountId // "") == $me | tostring)],
         ["_type", ($f | tickettype)],
         ["_project", (.key | split("-")[0] | ascii_downcase | gsub("[^a-z0-9]+"; "-"))],
         ["_parent_epic", (if $f.parent and (($f.parent.fields.issuetype.name // "") == "Epic" or ($f.parent.fields.issuetype.hierarchyLevel // 0) >= 1) then $f.parent.key else "" end)]]
      | .[] | @tsv' <<< "$issue" > "$dir/fm"
    local k v mine="" ptype="" project="" pepic=""
    while IFS=$'\t' read -r k v; do
      case "$k" in
        _mine) mine="$v" ;;
        _type) ptype="$v" ;;
        _project) project="$v" ;;
        _parent_epic) pepic="$v" ;;
        *) frontmatter_set "$note" "$k" "$v" ;;
      esac
    done < "$dir/fm"

    local cat_now status_now
    cat_now="$(frontmatter_get "$note" source-status-category)"
    status_now="$(frontmatter_get "$note" status)"
    if [[ "$mode" == create ]]; then
      frontmatter_set "$note" source jira
      frontmatter_set "$note" source-id "$key"
      frontmatter_set "$note" source-url "$jira_url/browse/$key"
      frontmatter_set "$note" ticket-type "$ptype"
      frontmatter_set "$note" status new
      frontmatter_set "$note" created "$today"
      add_tag "$note" jira
      add_tag "$note" "$project"
      [[ "$reason" == parent-epic ]] && add_tag "$note" parent-epic
    fi
    if [[ "$mode" == close || "$cat_now" == "done" ]]; then
      [[ "$status_now" == closed ]] || { frontmatter_set "$note" status closed; changes+=("closed"); }
    elif [[ "$status_now" == closed ]]; then
      frontmatter_set "$note" status new
      changes+=("reopened")
      followup "[[$key]] was reopened in Jira (status: new again); check whether work should resume"
    fi
    if [[ "$mine" == true ]]; then
      remove_tag "$note" unassigned
    elif [[ "$(frontmatter_get "$note" status)" != closed && "$reason" != parent-epic ]] && ! grep -q '^tags:.*parent-epic' "$note"; then
      grep -q '^tags:.*[[ ,]unassigned[],]' "$note" || changes+=("no longer yours")
      add_tag "$note" unassigned
    fi
    # A parent epic with no note is pulled in (tagged parent-epic)
    if [[ -n "$pepic" && ! -f "$(ticket_note "$vault" "$pepic")" ]]; then
      echo "$pepic" >> "$work/parent-epics"
    fi
  fi

  # Description (and text custom fields)
  if [[ "$desc" == 1 ]]; then
    local ev
    ev="$(jira_issue "$key" '{"view": "evidence"}')" || return 1
    if [[ "$(jq -r '.appliedContentFormat // "markdown"' <<< "$ev")" != markdown ]]; then
      jq -nc --arg k "$key" '{id: $k, part: "description"}' >> "$work/html.jsonl"
      changes+=("description (HTML, to Claude)")
    else
      jq -r --argjson text "$text_fields" '
        (.fields.description // "" | sub("\\s+$"; "")) as $d
        | (.fields.customFields // {}) as $c
        | (["### Description", (if $d == "" then "_No description._" else $d end)]
           + [$text[] | . as $label | $c[$label].value | select(type == "string" and test("\\S"))
              | "", "**\($label):** \(sub("\\s+$"; ""))"])
        | join("\n")' <<< "$ev" > "$dir/description"
    fi
  fi

  # Comments: rewritten when the last five's ids or timestamps changed
  if [[ "$reason" != children || "$all" == 1 ]]; then
    local cm sig old
    cm="$(jira_comments "$key")" || return 1
    sig="$(jq -r '"<!-- comments: \(if (.comments // []) | length == 0 then "none" else ((.comments | sort_by(.created)) | map("\(.id)@\(.updated)") | join(",")) end) -->"' <<< "$cm")"
    old="$(grep -o '<!-- comments: .* -->' "$dir/comments" 2>/dev/null || true)"
    if [[ "$sig" != "$old" ]]; then
      if [[ "$(jq -r '.appliedContentFormat // "markdown"' <<< "$cm")" != markdown ]]; then
        jq -nc --arg k "$key" --arg m "$sig" '{id: $k, part: "comments", marker: $m}' >> "$work/html.jsonl"
        changes+=("comments (HTML, to Claude)")
      else
        jq -r --arg sig "$sig" '
          (.comments // [] | sort_by(.created)) as $cs
          | (["### Recent comments"]
             + (if ($cs | length) == 0 then ["_No comments._"] else
                  [$cs[] | (.body // "" | sub("\\s+$"; "") | split("\n")) as $lines
                   | "- **\(.author.displayName // "unknown")**, \(.created[0:10]): \($lines[0] // "")",
                     ($lines[1:15][] | if . == "" then "" else "  " + . end)]
                end)
             + [$sig])
          | join("\n")' <<< "$cm" > "$dir/comments"
        [[ "$all" == 1 ]] || changes+=("comments")
      fi
    fi
  fi

  # An epic's children
  if [[ "$(frontmatter_get "$note" ticket-type)" == epic && ( "$table" == 1 || "$reason" == children ) ]]; then
    jira_write_children "$key" "$note" "$dir" || return 1
    [[ "$reason" == children ]] && changes+=("children")
  fi

  block_write "$note" "$dir"
  [[ -n "$updated" ]] && frontmatter_set "$note" source-updated "$updated"
  frontmatter_set "$note" updated "$today"

  local what
  case "$mode" in
    create) what="new" ;;
    close) what="closed" ;;
    *) if [[ ${#changes[@]} -eq 0 ]]; then what="nothing the note shows (timestamp only)"; else what="$(IFS=";"; echo "${changes[*]}")"; what="${what//;/; }"; fi ;;
  esac
  say "$key: $what [$(frontmatter_get "$note" source-status)] $(frontmatter_get "$note" summary)"
}

# jira_apply_plan: every create / refresh / close in $work/plan.json, then
# parent epics they pulled in. Failures become follow-up tasks.
jira_apply_plan() {
  local key reason failed=0
  : > "$work/html.jsonl"
  : > "$work/parent-epics"
  while IFS=$'\t' read -r key reason; do
    [[ -n "$key" ]] || continue
    case "$reason" in
      +create) jira_apply "$key" create "" ;;
      +close) jira_apply "$key" close "" ;;
      *) jira_apply "$key" refresh "$reason" ;;
    esac || {
      failed=1
      say "$key: couldn't sync (see the messages above)"
      followup "Check [[$key]]: ticket-sync couldn't update it; see ~/.local/state/$(basename "$log")"
    }
  done < <(jq -r '(.create[] | [., "+create"]), (.close[] | [., "+close"]), (.refresh[] | [.id, .reason]) | @tsv' "$work/plan.json")
  sort -u "$work/parent-epics" | while IFS= read -r key; do
    [[ -f "$(ticket_note "$vault" "$key")" ]] && continue
    jira_apply "$key" create parent-epic || {
      say "$key: couldn't pull in the parent epic"
      followup "Check [[$key]]: ticket-sync couldn't create this parent epic's note"
    }
  done
  return "$failed"
}
