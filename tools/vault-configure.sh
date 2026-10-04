usage() {
  cat <<'EOF'
Usage: vault-configure [<vault>] [--section <name>]... [--missing]
                       [--defaults] [--allow-overlap]

Configures a vault for the ticket workflow, one section at a time. Every
prompt shows the current value as its default, so it changes an existing
vault as easily as it sets up a new one (vault-init runs it). <vault> is a
name under ~/Documents/Obsidian or a path; default: the current vault
(vault-default).

Sections (all of them unless --section is given):
  locations  .workflow.json: where the vault's tools work
               workRoot      ticket and kb workspaces (~/work/<vault>)
               projectsRoot  main clones (~/Projects)
             (The ticket windows' tmux session is always tickets-<vault>.)
             A folder that is the same as, inside, or around another
             vault's folder (or this vault's other one) is shown with what
             it overlaps, and kept only when you type `yes`. Vaults sharing
             projectsRoot share their main clones, which is fine when meant;
             sharing workRoot mixes their workspaces (a MAN-1 exists in
             each). Moving workRoot doesn't move the workspaces already in the
             old one.
  sources    tickets/.sources.json: the ticket sources (manual, Jira). The
             old file is kept as tickets/.sources.json.bak; settings the
             prompts don't cover (other sources, model, textFields) are
             kept.
  runtime    podman or docker for the code graph, host-wide
             (~/.config/claude-tickets/config.json; default: detected,
             CLAUDE_TICKETS_CONTAINER overrides it)

  --section <name>  configure only this section (repeatable)
  --missing         only the sections not configured yet (vault-init)
  --defaults        don't prompt: keep sections already configured, write
                    the defaults for the others (also without a terminal)
  --allow-overlap   with --defaults: accept overlapping folders instead of
                    failing
EOF
}

all_sections=(locations sources runtime)
target="" sections=() missing=0 defaults=0 allow_overlap=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --section)
      s="$(need_val "$@")"
      [[ " ${all_sections[*]} " == *" $s "* ]] || die "no section '$s' (sections: ${all_sections[*]})"
      sections+=("$s")
      shift 2
      ;;
    --missing) missing=1; shift ;;
    --defaults) defaults=1; shift ;;
    --allow-overlap) allow_overlap=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) usage >&2; exit 1 ;;
    *) [[ -z "$target" ]] || die "give one vault"; target="$1"; shift ;;
  esac
done
[[ -t 0 ]] || defaults=1
[[ ${#sections[@]} -gt 0 ]] || sections=("${all_sections[@]}")

if [[ -n "$target" ]]; then
  vault="$(select_vault "$target" --no-default)"
else
  vault="$(current_vault)" || exit 1
fi
name="$(basename "$vault")"

# A path as it's shown and stored: $HOME as ~
tilde() {
  if [[ "$1" == "$HOME" || "$1" == "$HOME"/* ]]; then
    echo "~${1#"$HOME"}"
  else
    echo "$1"
  fi
}

# --- locations ---
locations_file() { echo "$vault/.workflow.json"; }

configure_locations() {
  local file w p cur_w cur_p v ow op overlaps old_ws
  file="$(locations_file)"
  IFS=$'\t' read -r cur_w cur_p _ < <(vault_locations "$vault")
  local old_w="$cur_w"
  [[ "$defaults" == 0 ]] && echo && echo "Locations ($(tilde "$file")); press Enter to keep the value shown."
  while true; do
    if [[ "$defaults" == 1 ]]; then
      w="$cur_w" p="$cur_p"
    else
      w="$(ask "  workRoot: ticket and kb workspaces" "$(tilde "$cur_w")")"
      p="$(ask "  projectsRoot: main clones" "$(tilde "$cur_p")")"
      w="${w/#\~/$HOME}" p="${p/#\~/$HOME}"
      # Asked again (after a refused overlap), the answers are the defaults
      cur_w="$w" cur_p="$p"
    fi

    overlaps=()
    paths_overlap "$w" "$p" \
      && overlaps+=("workRoot $(tilde "$w") and projectsRoot $(tilde "$p") overlap each other")
    while IFS= read -r v; do
      [[ "$(realpath -m "$obsidian_root/$v")" != "$(realpath -m "$vault")" ]] || continue
      IFS=$'\t' read -r ow op _ < <(vault_locations "$obsidian_root/$v")
      paths_overlap "$w" "$ow" \
        && overlaps+=("workRoot $(tilde "$w") overlaps $v's workRoot $(tilde "$ow"): the two vaults' workspaces mix, and a MAN-1 exists in each")
      paths_overlap "$w" "$op" \
        && overlaps+=("workRoot $(tilde "$w") overlaps $v's projectsRoot $(tilde "$op")")
      paths_overlap "$p" "$ow" \
        && overlaps+=("projectsRoot $(tilde "$p") overlaps $v's workRoot $(tilde "$ow")")
      paths_overlap "$p" "$op" \
        && overlaps+=("projectsRoot $(tilde "$p") overlaps $v's projectsRoot $(tilde "$op"): the vaults share main clones")
    done < <(list_vaults)

    if [[ ${#overlaps[@]} -gt 0 ]]; then
      printf '  ! %s\n' "${overlaps[@]}" >&2
      if [[ "$defaults" == 1 ]]; then
        [[ "$allow_overlap" == 1 ]] || die "overlapping folders (--allow-overlap to accept them, or run vault-configure in a terminal)"
      elif ! confirm_yes "  Keep these overlapping folders?"; then
        echo "  Then pick other folders."
        continue
      fi
    fi

    # Workspaces stay where they are when workRoot moves
    if [[ -f "$file" && "$(realpath -m "$w")" != "$(realpath -m "$old_w")" ]]; then
      mapfile -t old_ws < <(compgen -G "$old_w/*/workspace.json"; compgen -G "$old_w/.kb-*/workspace.json")
      if [[ ${#old_ws[@]} -gt 0 ]]; then
        echo "  ! these workspaces stay in the old workRoot $(tilde "$old_w"), and the ticket tools won't see them there:" >&2
        printf '      %s\n' "${old_ws[@]%/workspace.json}" >&2
        if [[ "$defaults" == 1 ]] || ! confirm_yes "  Move workRoot anyway (move them yourself after)?"; then
          continue
        fi
      fi
    fi
    break
  done

  # shellcheck disable=SC2016 # a jq filter
  jq -n --arg w "$(tilde "$w")" --arg p "$(tilde "$p")" '{
    _comment: "Where the ticket tools of this vault work: workRoot holds the ticket and kb workspaces, projectsRoot the main clones (vaults may share it). The ticket windows run in the tmux session tickets-<vault>. Change them with vault-configure --section locations, which checks for overlaps with other vaults.",
    workRoot: $w, projectsRoot: $p}' > "$file.tmp"
  mv "$file.tmp" "$file"
  echo "vault-configure: wrote .workflow.json (workspaces in $(tilde "$w"), main clones in $(tilde "$p"), tmux session tickets-$name)"
}

# --- sources ---
sources_file() { echo "$vault/tickets/.sources.json"; }

configure_sources() {
  local file current registered jira_default
  local manual manual_prefix jira jira_mcp jira_prefix jira_query jira_site
  file="$(sources_file)"
  current="$(cat "$file" 2>/dev/null || true)"
  [[ -n "$current" ]] || current='{}'
  src_get() {  # src_get <jq path> <default>
    local v
    v="$(jq -r "$1 // empty" <<< "$current" 2>/dev/null || true)"
    echo "${v:-$2}"
  }
  manual="$(src_get '.sources.manual.enabled' true)"
  manual_prefix="$(src_get '.sources.manual.idPrefix' MAN)"
  jira="$(src_get '.sources.jira.enabled' false)"
  jira_mcp="$(src_get '.sources.jira.mcp' atlassian)"
  jira_prefix="$(src_get '.sources.jira.idPrefix' '')"
  jira_query="$(src_get '.sources.jira.query' 'assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC')"
  jira_site="$(src_get '.sources.jira.site' '')"

  if [[ "$defaults" == 0 ]]; then
    echo
    echo "Ticket sources ($(tilde "$file")); press Enter to keep the value shown."
    if yes_no "Manual tickets, written in Obsidian (ticket-new)?" "$([[ "$manual" == true ]] && echo y || echo n)"; then
      manual=true
      while true; do
        manual_prefix="$(ask "  ID prefix for manual tickets (<prefix>-<n>)" "$manual_prefix")"
        valid_key "$manual_prefix-1" && break
        echo "  An ID prefix is upper-case letters/digits starting with a letter, e.g. MAN"
      done
    else
      manual=false
    fi
    registered=""
    registered="$(mcp_servers | paste -sd' ' || true)"
    if [[ -f "$file" ]]; then
      jira_default="$([[ "$jira" == true ]] && echo y || echo n)"
    else
      jira_default=n
      [[ " $registered " == *" atlassian "* ]] && jira_default=y
    fi
    if yes_no "Pull Jira tickets assigned to you (ticket-sync)?" "$jira_default"; then
      jira=true
      [[ -n "$registered" ]] && echo "  MCP servers in ${CLAUDE_TICKETS_MCP_CONFIG:-}: $registered"
      jira_mcp="$(ask "  MCP server for Jira (its name in CLAUDE_TICKETS_MCP_CONFIG)" "$jira_mcp")"
      if [[ " $registered " != *" $jira_mcp "* ]]; then
        warn "'$jira_mcp' isn't in CLAUDE_TICKETS_MCP_CONFIG (${CLAUDE_TICKETS_MCP_CONFIG:-unset}) yet; see references/atlassian-rovo-mcp-setup.md"
      fi
      jira_query="$(ask "  JQL for your open tickets" "$jira_query")"
      jira_site="$(ask "  Jira site, only if the token reaches several (- = find it)" "${jira_site:--}")"
      [[ "$jira_site" != - ]] || jira_site=""
    else
      jira=false
    fi
  fi

  # A new file starts from the full template (every known source, disabled);
  # an existing one keeps what the prompts don't cover
  if [[ ! -f "$file" ]]; then
    current='{
      "_comment": "Ticket sources for this vault. ticket-sync pulls every enabled source whose sync is not false, through the MCP server named by mcp (its URL comes from the MCP config in CLAUDE_TICKETS_MCP_CONFIG; the headless Claude step uses the servers your claude command provides). A source also needs an adapter: plugin/skills/ticket-sync/sources/<name>.md in claude-tickets. Local ticket IDs are the source key when idPrefix is empty, otherwise <idPrefix>-<native id>. manual tickets are written in Obsidian (ticket-new, templates/ticket-manual.md) and never synced. The jira source finds its Jira site from the token; set \"site\": \"<name>.atlassian.net\" only if the token can reach several. Change the manual and jira settings with vault-configure --section sources.",
      "sources": {
        "jira": {},
        "manual": {},
        "servicenow": {"enabled": false, "mcp": "servicenow", "idPrefix": "SNOW",
                       "query": "assigned_to=javascript:gs.getUserID()^active=true"},
        "zendesk": {"enabled": false, "mcp": "zendesk", "idPrefix": "ZD",
                    "query": "assignee:me status<solved"}
      }
    }'
  else
    cp "$file" "$file.bak"
  fi
  mkdir -p "$(dirname "$file")"
  # shellcheck disable=SC2016 # a jq filter
  jq --argjson manual "$manual" --arg mprefix "$manual_prefix" \
     --argjson jira "$jira" --arg mcp "$jira_mcp" --arg jprefix "$jira_prefix" \
     --arg query "$jira_query" --arg site "$jira_site" '
    .sources.jira = ((.sources.jira // {}) + {enabled: $jira, mcp: $mcp, idPrefix: $jprefix, query: $query})
    | if $site == "" then del(.sources.jira.site) else .sources.jira.site = $site end
    | .sources.manual = ((.sources.manual // {}) + {enabled: $manual, sync: false, idPrefix: $mprefix})
  ' <<< "$current" > "$file.tmp"
  mv "$file.tmp" "$file"
  echo "vault-configure: wrote tickets/.sources.json (jira: $jira, manual: $manual)"
}

# --- runtime (host-wide, not per vault) ---
runtime_file() { echo "$config_file"; }
runtime_configured() { [[ -n "$(config_get container)" ]]; }

configure_runtime() {
  local current detected rt
  current="$(config_get container)"
  detected="$(detect_container_runtime || true)"
  if [[ "$defaults" == 1 ]]; then
    rt="${current:-$detected}"
  else
    echo
    echo "Container runtime for the code graph (graphify), host-wide ($(tilde "$config_file"))."
    [[ -n "$detected" ]] && echo "  Detected: $detected"
    while true; do
      rt="$(ask "  podman or docker" "${current:-${detected:-podman}}")"
      [[ "$rt" == podman || "$rt" == docker ]] && break
      echo "  It's podman or docker."
    done
    command -v "$rt" >/dev/null || warn "$rt isn't installed yet"
  fi
  if [[ -z "$rt" ]]; then
    warn "no container runtime found (podman or docker); the code graph needs one"
    return 0
  fi
  config_set container "$rt"
  echo "vault-configure: container runtime is $rt (CLAUDE_TICKETS_CONTAINER overrides it)"
}

echo "vault-configure: $vault"
for s in "${sections[@]}"; do
  file="$("${s}_file")"
  if declare -F "${s}_configured" >/dev/null; then
    configured() { "${s}_configured"; }
  else
    configured() { [[ -f "$file" ]]; }
  fi
  if configured && [[ "$missing" == 1 || "$defaults" == 1 ]]; then
    echo "vault-configure: kept $(tilde "$file") (vault-configure --section $s to change it)"
    continue
  fi
  "configure_$s"
done

# The vault the commands act on
# (the saved default, not the vault a calling vault-init passed down)
if [[ "$defaults" == 0 && "$(launch_vault="" default_vault 2>/dev/null)" != "$vault" ]]; then
  echo
  if yes_no "Make $name the default vault, the one the ticket commands act on?" y; then
    echo "vault-configure: default vault is now $(set_default_vault "$vault")"
  fi
fi
