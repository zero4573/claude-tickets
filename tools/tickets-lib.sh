# Shared helpers for the claude-tickets commands (prepended to each one by
# nix/tools.nix and install.sh).
#
# The vault. Every command acts on the current vault (current_vault: the
# default vault, see vault-default); switch the default to work on another
# vault. Each vault says where its tools work in <vault>/.workflow.json
# (see vault_locations), applied at the end of this file. The sessions the
# commands start get the vault as CLAUDE_TICKETS_VAULT, so the tools they
# run act on the same vault even after the default changes, and wherever
# the default-vault file isn't visible.
#
# Environment (all optional):
#   OBSIDIAN_ROOT                    vaults live in $OBSIDIAN_ROOT/<name>
#                                    (~/Documents/Obsidian)
#   CLAUDE_TICKETS_CLAUDE            the claude command sessions run (claude)
#   CLAUDE_TICKETS_PLUGIN            the Claude Code plugin dir (skills,
#                                    agents, hooks); set by the package
#   CLAUDE_TICKETS_SESSION_SETTINGS  a Claude settings JSON file merged into
#                                    every session's .claude/settings.json
#                                    (your own rules: see the README)
#   CLAUDE_TICKETS_MCP_CONFIG        a standard MCP config naming the URL of
#                                    each server the ticket sources use
#   CLAUDE_TICKETS_MCP_PREPARE       command run before talking to those
#                                    servers (e.g. starting their proxy)
#   CLAUDE_TICKETS_CONTAINER         podman or docker (default: detected)
#   CLAUDE_TICKETS_SYSTEMD_SLICE     systemd user slice for containers
#   CLAUDE_TICKETS_CACHE             cache dir (~/.cache/claude-tickets)
# shellcheck disable=SC2034 # not every script uses every path
obsidian_root="${OBSIDIAN_ROOT:-$HOME/Documents/Obsidian}"
# shellcheck disable=SC2034
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/claude-tickets"
# shellcheck disable=SC2034
config_file="$config_dir/config.json"
# shellcheck disable=SC2034
cache_dir="${CLAUDE_TICKETS_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/claude-tickets}"
# The vault this command was started with (CLAUDE_TICKETS_VAULT), before
# use_vault exports its own
launch_vault="${CLAUDE_TICKETS_VAULT:-}"

# macOS: the scripts use GNU tools (realpath -m, stat -c, sed -i, date -I,
# find -printf); with Homebrew's coreutils, gnu-sed, findutils and gawk
# installed, use their g-prefixed names. (Nix installs bring GNU tools
# already.)
if [[ "$(uname -s)" == Darwin ]]; then
  for t in realpath stat sed date find xargs readlink; do
    if command -v "g$t" >/dev/null 2>&1; then
      eval "$t() { g$t \"\$@\"; }"
    fi
  done
  command -v gawk >/dev/null 2>&1 && awk() { gawk "$@"; }
fi

# Names of the servers in $CLAUDE_TICKETS_MCP_CONFIG, one per line
# shellcheck disable=SC2329 # shared helper; not every script uses it
mcp_servers() {
  [[ -n "${CLAUDE_TICKETS_MCP_CONFIG:-}" ]] || return 0
  jq -r '.mcpServers // {} | keys[]' "$CLAUDE_TICKETS_MCP_CONFIG" 2>/dev/null || true
}

# A key of ~/.config/claude-tickets/config.json (empty if unset):
# config_get <key>
# shellcheck disable=SC2329 # shared helper; not every script uses it
config_get() {
  jq -r --arg k "$1" '.[$k] // empty' "$config_file" 2>/dev/null || true
}
# Sets a key of config.json: config_set <key> <value>
# shellcheck disable=SC2329 # shared helper; not every script uses it
config_set() {
  mkdir -p "$config_dir"
  [[ -s "$config_file" ]] || echo '{}' > "$config_file"
  jq --arg k "$1" --arg v "$2" '.[$k] = $v' "$config_file" > "$config_file.tmp"
  mv "$config_file.tmp" "$config_file"
}

# Until a vault is applied: the locations of a run without one (the tmux
# session isn't used then: ticket-start and friends require a vault)
# shellcheck disable=SC2034
work_root="$HOME/work"
# shellcheck disable=SC2034
projects_root="$HOME/Projects"
# shellcheck disable=SC2034
tmux_session="tickets"

# shellcheck disable=SC2329 # shared helper; not every script uses it
die() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

# shellcheck disable=SC2329 # shared helper; not every script uses it
warn() {
  echo "$(basename "$0"): $*" >&2
}

# Local ticket IDs: the source's own key (Jira PROJ-12), or
# <idPrefix>-<native id> (SNOW-INC0012345, ZD-48213, MAN-7); see the
# vault's tickets/.sources.json
# shellcheck disable=SC2329 # shared helper; not every script uses it
valid_key() {
  [[ "$1" =~ ^[A-Z][A-Z0-9_]*(-[A-Z0-9]+)+$ ]]
}

# shellcheck disable=SC2329 # shared helper; not every script uses it
kebab() {
  tr '[:upper:]' '[:lower:]' <<< "$1" | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
}

# Parses a git remote URL into "<provider>\t<owner>\t<repo>" (nothing if it
# isn't a hosted remote):
#   bitbucket  Bitbucket Server/DC (/scm/<key>/<repo>, ssh :7999/<key>/<repo>;
#              owner = project key, upper-cased) or Cloud (bitbucket.org;
#              owner = workspace)
#   github     github.com or a GitHub Enterprise host (owner = user/org)
#   gitlab     gitlab hosts (owner = group, subgroups joined with -)
#   otherwise  the host name, kebab-cased (owner = the path before the repo)
# shellcheck disable=SC2329 # shared helper; not every script uses it
remote_identity() {
  local url="$1" rest host path provider owner repo server=0
  url="${url%/}"
  url="${url%.git}"
  case "$url" in
    *://*)
      rest="${url#*://}"
      rest="${rest#*@}"
      host="${rest%%/*}"
      path="${rest#*/}"
      ;;
    *@*:*)
      rest="${url#*@}"
      host="${rest%%:*}"
      path="${rest#*:}"
      ;;
    *) return 0 ;;
  esac
  [[ "$path" != "$rest" && -n "$path" ]] || return 0
  [[ "$host" == *:7999 ]] && server=1
  host="$(tr '[:upper:]' '[:lower:]' <<< "${host%%:*}")"
  if [[ "$path" == scm/* ]]; then
    path="${path#scm/}"
    server=1
  fi
  case "$host" in
    bitbucket.org) provider=bitbucket ;;
    *bitbucket*) provider=bitbucket; server=1 ;;
    *github*) provider=github ;;
    *gitlab*) provider=gitlab ;;
    *) provider="$(kebab "$host")" ;;
  esac
  [[ "$server" == 1 ]] && provider=bitbucket
  repo="${path##*/}"
  owner="${path%/*}"
  [[ -n "$repo" && -n "$owner" && "$owner" != "$path" ]] || return 0
  owner="${owner//\//-}"
  [[ "$provider" == bitbucket && "$host" != bitbucket.org ]] \
    && owner="$(tr '[:lower:]' '[:upper:]' <<< "$owner")"
  printf '%s\t%s\t%s\n' "$provider" "$owner" "$repo"
}

# The repo's slug, used wherever the vault names a repo (projects/<slug>/,
# tags, graph tags, worktree folders): <provider>-<owner>-<repo>, kebab-case.
# Takes "<provider>/<owner>/<repo>" (a main clone's path under ~/Projects).
# shellcheck disable=SC2329 # shared helper; not every script uses it
repo_slug() {
  kebab "${1//\//-}"
}

# Main clones under $projects_root, as <provider>/<owner>/<repo>
# shellcheck disable=SC2329 # shared helper; not every script uses it
list_main_clones() {
  local g
  for g in "$projects_root"/*/*/*/.git; do
    [[ -d "$g" ]] || continue
    g="${g%/.git}"
    echo "${g#"$projects_root"/}"
  done
}

# Resolves a repo given as a slug or as <provider>/<owner>/<repo> to the
# latter; fails if no such main clone exists
# shellcheck disable=SC2329 # shared helper; not every script uses it
resolve_repo() {
  local spec="$1" r
  if [[ "$spec" == */*/* && -d "$projects_root/$spec/.git" ]]; then
    echo "$spec"
    return 0
  fi
  while IFS= read -r r; do
    if [[ "$(repo_slug "$r")" == "$spec" ]]; then
      echo "$r"
      return 0
    fi
  done < <(list_main_clones)
  return 1
}

# Every vault: a directory under $obsidian_root holding .obsidian/
# shellcheck disable=SC2329 # shared helper; not every script uses it
list_vaults() {
  local d
  for d in "$obsidian_root"/*/; do
    [[ -d "$d.obsidian" ]] && basename "$d"
  done
}

# The default vault (see vault-default): $launch_vault (a session's vault,
# or the caller's when one ticket tool runs another), else the vault of the
# ticket or kb workspace the current directory is in (its workspace.json),
# else the name saved in $default_vault_file. Prints it only if it's an
# existing vault.
# shellcheck disable=SC2034
default_vault_file="$config_dir/default-vault"
# The location before claude-tickets had its own config dir
if [[ ! -e "$default_vault_file" && -s "${XDG_CONFIG_HOME:-$HOME/.config}/tickets/default-vault" ]]; then
  default_vault_file="${XDG_CONFIG_HOME:-$HOME/.config}/tickets/default-vault"
fi
# shellcheck disable=SC2329 # shared helper; not every script uses it
default_vault() {
  local name="$launch_vault" d="$PWD"
  while [[ -z "$name" && -n "$d" && "$d" != / ]]; do
    name="$(jq -r '.vault // empty' "$d/workspace.json" 2>/dev/null || true)"
    d="$(dirname "$d")"
  done
  [[ -n "$name" ]] || name="$(cat "$default_vault_file" 2>/dev/null || true)"
  [[ -n "$name" ]] || return 1
  if [[ -d "$name/.obsidian" ]]; then
    realpath "$name"
  elif [[ -d "$obsidian_root/$name/.obsidian" ]]; then
    echo "$obsidian_root/$name"
  else
    warn "default vault '$name' isn't a vault (vault-default to change it), ignoring it"
    return 1
  fi
}

# The vault the commands act on: the default vault, else the only vault.
# Fails, saying how to set one, when there are several and no default.
# shellcheck disable=SC2329 # shared helper; not every script uses it
current_vault() {
  local vaults=()
  default_vault && return
  mapfile -t vaults < <(list_vaults)
  [[ ${#vaults[@]} -gt 0 ]] || die "no Obsidian vaults under $obsidian_root (set one up with vault-init)"
  [[ ${#vaults[@]} -eq 1 ]] \
    || die "no default vault, and several under $obsidian_root (${vaults[*]}): pick one with vault-default <name>"
  echo "$obsidian_root/${vaults[0]}"
}

# Sets $vault to the current vault and applies its locations (use_vault),
# or exits with current_vault's message
# shellcheck disable=SC2329 # shared helper; not every script uses it
require_vault() {
  vault="$(current_vault)" || exit 1
  use_vault "$vault"
}

# Makes <vault path> the default vault: stores its name when it lives under
# $obsidian_root, else its path. Prints what was stored.
# shellcheck disable=SC2329 # shared helper; not every script uses it
set_default_vault() {
  # Always the current location (and the old one goes away)
  rm -f "${XDG_CONFIG_HOME:-$HOME/.config}/tickets/default-vault"
  default_vault_file="$config_dir/default-vault"
  mkdir -p "$config_dir"
  if [[ "$(dirname "$1")" == "$obsidian_root" ]]; then
    basename "$1" > "$default_vault_file"
  else
    echo "$1" > "$default_vault_file"
  fi
  cat "$default_vault_file"
}

# Interactive prompts (vault-init, vault-configure)
# ask <prompt> <default>: prints the answer (the default on Enter)
# shellcheck disable=SC2329 # shared helper; not every script uses it
ask() {
  local reply
  read -rp "$1 [$2] " reply
  echo "${reply:-$2}"
}
# yes_no <prompt> <y|n>: true for yes
# shellcheck disable=SC2329 # shared helper; not every script uses it
yes_no() {
  local reply
  reply="$(ask "$1 (y/n)" "$2")"
  [[ "$reply" == [yY]* ]]
}
# confirm_yes <prompt>: true only when `yes` is typed in full (no default)
# shellcheck disable=SC2329 # shared helper; not every script uses it
confirm_yes() {
  local reply
  read -rp "$1 Type yes to confirm: " reply
  [[ "$reply" == yes ]]
}

# For the vault-* commands, which name or pick a vault instead of using the
# current one: the absolute path of $1 if given (a name under
# $obsidian_root or a path), else the default vault (unless $2 is
# --no-default), else the only vault, else an fzf pick
# shellcheck disable=SC2329 # shared helper; not every script uses it
select_vault() {
  local name="${1:-}" use_default="${2:-}" vaults=() count
  if [[ -z "$name" && "$use_default" != --no-default ]] && default_vault; then
    return
  fi
  if [[ -n "$name" ]]; then
    if [[ -d "$name/.obsidian" ]]; then
      realpath "$name"
      return
    fi
    [[ -d "$obsidian_root/$name/.obsidian" ]] || die "no vault named '$name' under $obsidian_root"
    echo "$obsidian_root/$name"
    return
  fi
  mapfile -t vaults < <(list_vaults)
  count="${#vaults[@]}"
  [[ "$count" -gt 0 ]] || die "no Obsidian vaults under $obsidian_root"
  if [[ "$count" -eq 1 ]]; then
    echo "$obsidian_root/${vaults[0]}"
    return
  fi
  [[ -t 0 && -t 2 ]] || die "several vaults under $obsidian_root (${vaults[*]}); name one, or set a default with vault-default <name>"
  name="$(printf '%s\n' "${vaults[@]}" | fzf --prompt='vault> ' --height=~10 --reverse)" \
    || die "no vault selected"
  echo "$obsidian_root/$name"
}

# Value of a top-level scalar field in a note's YAML frontmatter (empty if
# absent), with surrounding quotes stripped
# shellcheck disable=SC2329 # shared helper; not every script uses it
frontmatter_get() {
  local file="$1" field="$2"
  [[ -f "$file" ]] || return 0
  awk -v field="$field" '
    NR == 1 && $0 != "---" { exit }
    NR > 1 && $0 == "---" { exit }
    NR > 1 {
      split($0, kv, ":")
      if (kv[1] == field) {
        sub(/^[^:]*:[ \t]*/, "")
        gsub(/^["\x27]|["\x27][ \t]*$/, "")
        print
        exit
      }
    }' "$file"
}

# shellcheck disable=SC2329 # shared helper; not every script uses it
ticket_note() {
  echo "$1/tickets/$2/$2.md"
}

# The vault's tickets, one "<ID>\t<status>\t<summary>" line each (status
# gets ", ignored" when the note sets ignore: true), sorted by ID; done and
# closed tickets only with --all: list_tickets <vault> [--all]. One awk pass
# over every note's frontmatter (shell completion runs it on each Tab).
# shellcheck disable=SC2329 # shared helper; not every script uses it
list_tickets() {
  local vault="$1" all="${2:-}" d key notes=()
  for d in "$vault"/tickets/*/; do
    key="$(basename "$d")"
    valid_key "$key" && [[ -f "$d$key.md" ]] && notes+=("$d$key.md")
  done
  [[ ${#notes[@]} -gt 0 ]] || return 0
  awk -v all="$all" '
    function val() { v = $0; sub(/^[^:]*:[ \t]*/, "", v); gsub(/^["\x27]|["\x27][ \t]*$/, "", v); return v }
    BEGINFILE { key = FILENAME; sub(/.*\//, "", key); sub(/\.md$/, "", key)
                status = ""; summary = ""; ignore = ""; fm = 0 }
    FNR == 1 { if ($0 == "---") { fm = 1; next } else nextfile }
    fm && $0 == "---" { nextfile }
    fm && /^status:/ { status = val() }
    fm && /^summary:/ { summary = val() }
    fm && /^ignore:/ { ignore = val() }
    ENDFILE {
      if (all == "--all" || (status != "done" && status != "closed")) {
        if (ignore == "true") status = status ", ignored"
        gsub(/\t/, " ", summary)
        print key "\t" (status == "" ? "-" : status) "\t" summary
      }
    }' "${notes[@]}" | sort
}

# True when the ticket note sets ignore: true and any ignore-until date
# hasn't passed yet
# shellcheck disable=SC2329 # shared helper; not every script uses it
ticket_ignored() {
  local note="$1" ignore until
  ignore="$(frontmatter_get "$note" ignore)"
  [[ "$ignore" == true ]] || return 1
  until="$(frontmatter_get "$note" ignore-until)"
  [[ -z "$until" || ! "$until" < "$(date +%F)" ]]
}

# Headless runs (claude -p --output-format stream-json --verbose): turns the
# event stream on stdin into readable progress as it happens. The agent's
# text as it writes it, one line per tool call (with its key argument), and
# the final result. Lines that aren't JSON (the launcher's own messages)
# pass through as they are.
# shellcheck disable=SC2329 # shared helper; not every script uses it
claude_progress() {
  jq -Rrj --unbuffered '
    def brief: if type == "string" then .[0:100] | gsub("\n"; " ") else tostring end;
    def arg: (.jql // .issueIdOrKey // .file_path // .path // .pattern
              // .command // .name // .query // .q // "") | brief;
    . as $line | (try fromjson catch null) as $e
    | if $e == null then $line + "\n"
      elif $e.type == "assistant" then
        [ $e.message.content[]?
          | if .type == "text" and (.text | length) > 0 then .text + "\n"
            elif .type == "tool_use" then "  → \(.name | sub("^mcp__[^_]+__"; ""))  \(.input | arg)\n"
            else empty end ] | join("")
      elif $e.type == "result" then
        "\n" + (if $e.is_error then "ERROR: " else "" end) + ($e.result // "") + "\n"
      else empty end'
}

# Log file for a command's runs: ~/.local/state/<name>.log, trimmed when large
# shellcheck disable=SC2329 # shared helper; not every script uses it
state_log() {
  local dir="${XDG_STATE_HOME:-$HOME/.local/state}" log
  mkdir -p "$dir"
  log="$dir/$1.log"
  # Bounded: past 5 MB, keep the newest 1 MB
  if [[ -f "$log" && $(stat -c %s "$log") -gt 5242880 ]]; then
    tail -c 1048576 "$log" > "$log.tmp" && mv "$log.tmp" "$log"
  fi
  echo "$log"
}

# Follow-up note for an unattended command (ticket-sync, graphify-index,
# ticket-ws gc): when a run leaves something for you to check or do, it's
# written to <vault>/inbox/<date>-<time>-<command>-follow-ups.md as Tasks
# plugin tasks, so it shows in pending.md. type: follow-ups tells /tickets:save it
# isn't a knowledge draft. Prints the note's path; does nothing without
# tasks. followup_note <vault> <command> <what ran> <task file> [<link>...]
# where the task file holds one task per line (text only) and the links
# are note names to link besides the dashboard and pending.md.
# shellcheck disable=SC2329 # shared helper; not every script uses it
followup_note() {
  local vault="$1" cmd="$2" what="$3" tasks="$4" today stamp name note link
  shift 4
  [[ -s "$tasks" && -d "$vault/.obsidian" ]] || return 0
  today="$(date +%F)"
  stamp="$(date +%F-%H%M)"
  name="$stamp-$cmd-follow-ups"
  note="$vault/inbox/$name.md"
  mkdir -p "$vault/inbox"
  {
    printf -- '---\ntitle: %s\ncreated: %s\nupdated: %s\nstatus: active\ntype: follow-ups\nsource-command: %s\ntags: [follow-ups, %s]\n---\n' \
      "$name" "$today" "$today" "$cmd" "$cmd"
    printf '# %s: follow-ups\n\n%s left these for you (%s). Tick them off here; delete the note\nwhen done. Open tasks also show in [[pending]] and [[tickets.base|Tickets]].\n\n## Tasks\n' \
      "$cmd" "$what" "$(date '+%F %H:%M')"
    sort -u "$tasks" | while IFS= read -r task; do
      [[ -n "$task" ]] && printf -- '- [ ] %s #%s ➕ %s\n' "$task" "$cmd" "$today"
    done
    if [[ $# -gt 0 ]]; then
      printf '\n## Related\n'
      for link in "$@"; do printf -- '- [[%s]]\n' "$link"; done
    fi
  } > "$note"
  echo "$note"
}

# The value of an option: `x="$(need_val "$@")"; shift 2` with "$1" the
# option. Fails with a message instead of shift 2 erroring out silently
# shellcheck disable=SC2329 # shared helper; not every script uses it
need_val() {
  [[ $# -ge 2 && -n "$2" ]] || die "$1 needs a value"
  echo "$2"
}

# Runs a jq filter over a JSON file in place, under a lock, so concurrent
# writers (parallel subagents, kb-repo) don't lose each other's updates:
# json_update <file> [jq args...] <filter>
# shellcheck disable=SC2329 # shared helper; not every script uses it
json_update() {
  local file="$1"
  shift
  (
    flock 9
    jq "$@" "$file" > "$file.tmp"
    mv "$file.tmp" "$file"
  ) 9>"$file.lock"
}

# A session workspace's workspace.json (~/work/<ID>, ~/work/.kb-<vault>):
# ensure_workspace <dir> <id> <vault>. Keeps the repos already recorded.
# shellcheck disable=SC2329 # shared helper; not every script uses it
ensure_workspace() {
  local dir="$1" id="$2" vault="${3:-}" file="$1/workspace.json"
  mkdir -p "$dir"
  [[ -f "$file" ]] || jq -n --arg id "$id" '{id: $id, repos: []}' > "$file"
  # shellcheck disable=SC2016 # a jq filter
  [[ -z "$vault" ]] || json_update "$file" --arg v "$vault" '.vault = $v'
}

# Upserts a repo entry (by slug) into a workspace.json:
# workspace_put <file> <clone> <path> <base> <branch> <mode> [<targetVersion>]
# (clone = <provider>/<owner>/<repo>; an empty targetVersion keeps the old one)
# shellcheck disable=SC2329 # shared helper; not every script uses it
workspace_put() {
  local file="$1" clone="$2" provider owner repo
  provider="${clone%%/*}"
  owner="${clone#*/}"
  repo="${owner#*/}"
  owner="${owner%%/*}"
  # shellcheck disable=SC2016 # a jq filter
  json_update "$file" --arg provider "$provider" --arg owner "$owner" --arg repo "$repo" \
    --arg slug "$(repo_slug "$clone")" --arg path "$3" --arg base "$4" --arg branch "$5" \
    --arg mode "$6" --arg version "${7:-}" '
    .repos |= (map(select(.slug != $slug)) + [{
      provider: $provider, owner: $owner, repo: $repo, slug: $slug,
      path: $path, base: $base, branch: $branch, mode: $mode,
      targetVersion: (if $version == "" then ((.[] | select(.slug == $slug) | .targetVersion) // null) else $version end)
    }])'
}

# A shared clone of a main clone (git alternates: it borrows the main clone's
# objects and writes nothing there), with the main clone's remote-tracking
# refs and tags as its own and origin pointing at the real remote:
# shared_clone <main> <dest>
# shellcheck disable=SC2329 # shared helper; not every script uses it
shared_clone() {
  git clone --quiet --shared --no-checkout "$1" "$2"
  shared_clone_refresh "$1" "$2"
  git -C "$2" remote set-url origin "$(git -C "$1" remote get-url origin)"
  seed_graph "$1" "$2"
}

# Re-reads origin's branches and tags from the main clone (local, no
# credentials); --prune drops the refs the clone made of its local branches
# shellcheck disable=SC2329 # shared helper; not every script uses it
shared_clone_refresh() {
  git -C "$2" fetch --quiet --prune "$1" \
    '+refs/remotes/origin/*:refs/remotes/origin/*' '+refs/tags/*:refs/tags/*'
}

# Seeds a new checkout's code graph from its main clone, so graphify only
# re-extracts what differs. Only the current graph, manifest and cache, not
# graphify's dated snapshots.
# shellcheck disable=SC2329 # shared helper; not every script uses it
seed_graph() {
  local src="$1/graphify-out" dest="$2/graphify-out" f
  [[ -d "$src" && ! -e "$dest" ]] || return 0
  mkdir -p "$dest"
  for f in graph.json manifest.json cache; do
    [[ -e "$src/$f" ]] && cp -r "$src/$f" "$dest/"
  done
  return 0
}

# graphify keeps a dated snapshot folder per rebuild day in graphify-out/;
# drops the ones older than a week: prune_graph_snapshots <graphify-out>...
# shellcheck disable=SC2329 # shared helper; not every script uses it
prune_graph_snapshots() {
  local d
  for d in "$@"; do
    [[ -d "$d" ]] || continue
    find "$d" -mindepth 1 -maxdepth 1 -type d -name '20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]*' \
      -mtime +7 -exec rm -rf {} + 2>/dev/null || true
  done
}

# A session workspace's .claude/settings.json, which Claude reads as the
# project settings: write_session_settings <dir> [<deny rule>...]
#  * what the ticket system needs: the main clones are read-only (worktrees
#    exist so they stay untouched), and so is the graph image cache
#  * the given deny rules (e.g. kb's), and
#  * $CLAUDE_TICKETS_SESSION_SETTINGS, your own settings (see the README),
#    merged in: arrays concatenated, objects merged
# Regenerated on every start.
# shellcheck disable=SC2329 # shared helper; not every script uses it
write_session_settings() {
  local dir="$1" extra="{}" rules=() p
  shift
  for p in "$projects_root" "$cache_dir"; do
    rules+=("Edit(/$p/**)" "Write(/$p/**)" "NotebookEdit(/$p/**)")
  done
  if [[ -n "${CLAUDE_TICKETS_SESSION_SETTINGS:-}" ]]; then
    extra="$(jq -c . "$CLAUDE_TICKETS_SESSION_SETTINGS" 2>/dev/null)" \
      || die "CLAUDE_TICKETS_SESSION_SETTINGS ($CLAUDE_TICKETS_SESSION_SETTINGS) isn't a readable JSON file"
  fi
  mkdir -p "$dir/.claude"
  # shellcheck disable=SC2016 # a jq filter
  jq -n --argjson extra "$extra" --args '
    def merge(a; b): if (a | type) == "object" and (b | type) == "object"
      then reduce (b | keys_unsorted[]) as $k (a; .[$k] = merge(a[$k]; b[$k]))
      elif (a | type) == "array" and (b | type) == "array" then (a + b | unique)
      elif b == null then a else b end;
    merge({permissions: {deny: $ARGS.positional}}; $extra)' "${rules[@]}" "$@" \
    > "$dir/.claude/settings.json"
}

# Marks a workspace as trusted in Claude Code's own config
# (projects[<dir>].hasTrustDialogAccepted in ~/.claude.json, or
# $CLAUDE_CONFIG_DIR/.claude.json), so a session the tools start there gets
# straight to work instead of asking whether to trust the folder. Only for
# workspaces these tools create: trust_workspace <dir>
# shellcheck disable=SC2329 # shared helper; not every script uses it
trust_workspace() {
  local file="${CLAUDE_CONFIG_DIR:-$HOME}/.claude.json" dir
  dir="$(realpath "$1")"
  [[ -s "$file" ]] || echo '{}' > "$file"
  jq -e --arg d "$dir" '.projects[$d].hasTrustDialogAccepted == true' "$file" >/dev/null 2>&1 && return 0
  (
    flock 9
    # In place, not mv: running sandboxes bind-mount this very file
    jq --arg d "$dir" '.projects[$d].hasTrustDialogAccepted = true' "$file" > "$file.tickets-tmp" \
      && cat "$file.tickets-tmp" > "$file"
    rm -f "$file.tickets-tmp"
  ) 9>"$file.tickets-lock" || warn "couldn't mark $dir as trusted in $file"
  rm -f "$file.tickets-lock"
}

# The claude command a session runs, with the plugin (skills, role agents,
# hooks) and the directories it works in: claude_session_cmd <array name>
# <dir>... (each dir becomes an --add-dir). Uses $CLAUDE_TICKETS_CLAUDE.
# shellcheck disable=SC2329 # shared helper; not every script uses it
claude_session_cmd() {
  local -n session_cmd_out="$1"
  local d plugin
  shift
  plugin="$(plugin_dir)"
  read -ra session_cmd_out <<< "${CLAUDE_TICKETS_CLAUDE:-claude}"
  session_cmd_out+=(--plugin-dir "$plugin")
  for d in "$@"; do
    if [[ -d "$d" ]]; then session_cmd_out+=("--add-dir=$d"); fi
  done
}

# The Claude Code plugin shipped with these tools: $CLAUDE_TICKETS_PLUGIN,
# else config.json's pluginDir (install.sh), else next to the scripts
# shellcheck disable=SC2329 # shared helper; not every script uses it
plugin_dir() {
  local d="${CLAUDE_TICKETS_PLUGIN:-}"
  [[ -n "$d" ]] || d="$(config_get pluginDir)"
  [[ -n "$d" ]] || d="$(dirname "$(realpath "$0")")/../share/claude-tickets/plugin"
  [[ -f "$d/.claude-plugin/plugin.json" ]] || die "no claude-tickets plugin at $d (set CLAUDE_TICKETS_PLUGIN, or reinstall)"
  echo "$d"
}

# --- containers (graphify) ---

# podman or docker: $CLAUDE_TICKETS_CONTAINER, else config.json's
# container, else the first whose service answers (podman first)
# shellcheck disable=SC2329 # shared helper; not every script uses it
container_runtime() {
  local rt="${CLAUDE_TICKETS_CONTAINER:-}"
  [[ -n "$rt" ]] || rt="$(config_get container)"
  if [[ -n "$rt" ]]; then
    [[ "$rt" == podman || "$rt" == docker ]] || die "container runtime must be podman or docker, not '$rt'"
    command -v "$rt" >/dev/null || die "$rt isn't installed (CLAUDE_TICKETS_CONTAINER / vault-configure --section runtime)"
    # podman's docker alias (docker-compat) is podman: it needs podman's flags
    if [[ "$rt" == docker ]] && { [[ "$(basename "$(realpath "$(command -v docker)")")" == podman ]] \
         || docker --version 2>/dev/null | grep -qi podman; }; then
      rt=podman
    fi
    echo "$rt"
    return
  fi
  detect_container_runtime || die "no container runtime: install podman or docker"
}
# shellcheck disable=SC2329 # shared helper; not every script uses it
detect_container_runtime() {
  local rt
  for rt in podman docker; do
    command -v "$rt" >/dev/null 2>&1 && "$rt" info >/dev/null 2>&1 && { echo "$rt"; return 0; }
  done
  return 1
}

# Runs a container with the right flags for the runtime: container_run
# <run args...>. SELinux labelling is off rather than relabelling mounted
# trees; docker runs as you, so files it writes into mounts are yours
# (rootless podman maps its root to you already). In
# $CLAUDE_TICKETS_SYSTEMD_SLICE when set.
# shellcheck disable=SC2329 # shared helper; not every script uses it
container_run() {
  local rt pre=() flags=(--rm --security-opt label=disable)
  rt="$(container_runtime)"
  [[ "$rt" == docker ]] && flags+=(--user "$(id -u):$(id -g)")
  if [[ -n "${CLAUDE_TICKETS_SYSTEMD_SLICE:-}" ]] && command -v systemd-run >/dev/null; then
    pre=(systemd-run --user --scope --quiet --collect "--slice=$CLAUDE_TICKETS_SYSTEMD_SLICE" --)
    [[ "$rt" == podman ]] && flags+=("--cgroup-parent=$CLAUDE_TICKETS_SYSTEMD_SLICE")
  fi
  "${pre[@]}" "$rt" run "${flags[@]}" "$@"
}

# Whether a workspace's session is running: its window is open in the
# vault's tmux session (ticket sessions), or its hooks' last state isn't
# exited and is less than a day old (kb sessions run in your own terminal):
# session_running <workspace dir>
# shellcheck disable=SC2329 # shared helper; not every script uses it
session_running() {
  local dir="$1" key state
  key="$(basename "$dir")"
  tmux list-windows -t "=$tmux_session" -F '#W' 2>/dev/null | grep -qxF "$key" && return 0
  [[ -f "$dir/.agent-state" ]] || return 1
  state="$(jq -r '.state // empty' "$dir/.agent-state" 2>/dev/null || true)"
  [[ -n "$state" && "$state" != exited ]] || return 1
  [[ -n "$(find "$dir/.agent-state" -mmin -1440 2>/dev/null)" ]]
}

# Runs a headless claude command (claude -p ... --output-format
# stream-json --verbose) showing progress live and appending it to a log:
# run_headless <log> <command...>. Returns the command's exit code.
# shellcheck disable=SC2329 # shared helper; not every script uses it
run_headless() {
  local log="$1" code
  shift
  set +e
  "$@" < /dev/null 2>&1 | claude_progress | tee -a "$log"
  code="${PIPESTATUS[0]}"
  set -e
  return "$code"
}

# A vault's locations, so vaults never share workspaces (a MAN-1 exists in
# each) or a tmux session. From <vault>/.workflow.json (vault-configure
# writes it):
#   workRoot      ticket and kb workspaces   (default ~/work/<vault name>)
#   projectsRoot  main clones                (default ~/Projects; vaults may
#                                             share it)
# The tmux session is always tickets-<vault name>, so it says which vault
# its ticket windows belong to.
# Prints "<workRoot>\t<projectsRoot>\t<tmux session>", `~` expanded:
# vault_locations <vault path>
# shellcheck disable=SC2329 # shared helper; not every script uses it
vault_locations() {
  local name w p
  name="$(basename "$1")"
  # \x1f, not a tab: read collapses empty tab-separated fields
  IFS=$'\x1f' read -r w p < <(
    jq -r '[.workRoot // "", .projectsRoot // ""] | join("\u001f")' \
      "$1/.workflow.json" 2>/dev/null || printf '\x1f\n'
  ) || true
  w="${w:-$HOME/work/$name}"
  p="${p:-$HOME/Projects}"
  printf '%s\t%s\t%s\n' "${w/#\~/$HOME}" "${p/#\~/$HOME}" "tickets-$name"
}

# True when two paths are the same or one is inside the other, after `~`
# expansion and resolving symlinks: paths_overlap <a> <b>
# shellcheck disable=SC2329 # shared helper; not every script uses it
paths_overlap() {
  local a b
  a="$(realpath -m "${1/#\~/$HOME}")"
  b="$(realpath -m "${2/#\~/$HOME}")"
  [[ "$a" == "$b" || "$a" == "$b"/* || "$b" == "$a"/* ]]
}

# Applies a vault's locations (vault_locations) to $work_root,
# $projects_root and $tmux_session, and exports CLAUDE_TICKETS_VAULT, so the
# tools a session runs act on the same vault (they re-read its
# .workflow.json): use_vault <vault path>
# shellcheck disable=SC2329 # shared helper; not every script uses it
use_vault() {
  # shellcheck disable=SC2034 # not every script uses every location
  IFS=$'\t' read -r work_root projects_root tmux_session < <(vault_locations "$1")
  export CLAUDE_TICKETS_VAULT="$1"
}

# Until a script calls require_vault: the current vault's locations, if
# there is one
if lib_vault="$(current_vault 2>/dev/null)"; then
  use_vault "$lib_vault"
fi
