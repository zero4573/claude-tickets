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

# A key of ~/.config/claude-tickets/config.json (empty if unset):
# config_get <key>
# shellcheck disable=SC2329 # shared helper; not every script uses it
config_get() {
  jq -r --arg k "$1" '.[$k] // empty' "$config_file" 2>/dev/null || true
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
  [[ ${#vaults[@]} -gt 0 ]] || die "no Obsidian vaults under $obsidian_root (set one up with ct vault init)"
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

# Follow-up note for an unattended command (ticket-sync; ct graph index
# and ct ws gc write theirs in Go): when a run leaves something for you to check or do, it's
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
# each) or a tmux session. From <vault>/.workflow.json (ct vault configure
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
