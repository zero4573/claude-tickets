usage() {
  cat <<'EOF'
Usage: vault-init [<vault>] [--defaults] [--allow-overlap]

Sets an Obsidian vault up for the ticket workflow. <vault> is a name under
~/Documents/Obsidian (created if it doesn't exist) or a path; without it you
pick one of the existing vaults.

  * folders: raw, tickets, templates, inbox, logs, references,
    knowledge-base, projects/system/{architecture,sequences,features,data,logs}
  * files: AGENTS.md, templates/, tickets.base (dashboard), the task views
    (pending.md, done.md, follow-ups.md),
    projects/system notes, the Atlassian setup reference
  * Obsidian settings: the templates folder, the Bases / Templates /
    Properties core plugins, and the workflow's property types
  * its settings, through vault-configure: where its workspaces and main
    clones live (.workflow.json, checked for overlaps with other vaults)
    and which ticket sources it uses (tickets/.sources.json). Settings
    already made are kept; change them with vault-configure.

Existing files are never overwritten (they're listed as kept), so it's safe
to re-run, e.g. to add files a newer version of the workflow ships.

  --defaults       don't prompt: default locations, manual tickets on, Jira
                   configured but disabled
  --allow-overlap  with --defaults: accept default folders that overlap
                   another vault's (e.g. a shared ~/Projects)
EOF
}

target="" defaults=0 allow_overlap=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --allow-overlap) allow_overlap=1; shift ;;
    --defaults) defaults=1; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) usage >&2; exit 1 ;;
    *) [[ -z "$target" ]] || die "give one vault"; target="$1"; shift ;;
  esac
done
[[ -t 0 ]] || defaults=1

# --- the vault ---
if [[ -z "$target" ]]; then
  [[ -t 0 || $(list_vaults | wc -l) -le 1 ]] || die "several vaults; name one: vault-init <vault>"
  vault="$(select_vault "" --no-default)"
elif [[ "$target" == */* || -d "$target/.obsidian" ]]; then
  vault="$(realpath -m "$target")"
else
  vault="$obsidian_root/$target"
fi
if [[ ! -d "$vault/.obsidian" ]]; then
  if [[ "$defaults" == 0 ]] && ! yes_no "No vault at $vault yet. Create it?" y; then
    exit 1
  fi
  mkdir -p "$vault/.obsidian"
  echo "vault-init: created vault $vault (open it in Obsidian: Open folder as vault)"
fi
echo "vault-init: setting up $vault"

# --- folders ---
for d in raw tickets templates inbox logs references knowledge-base \
         projects/system/architecture projects/system/sequences \
         projects/system/features projects/system/data projects/system/logs; do
  mkdir -p "$vault/$d"
done

# --- files (never overwritten) ---
added=() kept=()
while IFS= read -r -d '' src; do
  rel="${src#"$VAULT_SCAFFOLD"/}"
  [[ "$rel" == obsidian-types.json ]] && continue
  if [[ -e "$vault/$rel" ]]; then
    kept+=("$rel")
  else
    mkdir -p "$(dirname "$vault/$rel")"
    install -m 0644 "$src" "$vault/$rel"
    added+=("$rel")
  fi
done < <(find "$VAULT_SCAFFOLD" -type f -print0 | sort -z)
echo "vault-init: added ${#added[@]} file(s), kept ${#kept[@]} existing"
[[ ${#added[@]} -eq 0 ]] || printf '  + %s\n' "${added[@]}"

# --- Obsidian settings ---
obs="$vault/.obsidian"
update_json() {  # update_json <file> <default json> <jq filter>
  local file="$1" current
  current="$(cat "$file" 2>/dev/null || true)"
  [[ -n "$current" ]] || current="$2"
  jq "$3" <<< "$current" > "$file.tmp"
  mv "$file.tmp" "$file"
}
# Core Templates plugin reads templates/; keep a folder the user already set
update_json "$obs/templates.json" '{}' '.folder //= "templates"'
if [[ "$(jq -r '.folder' "$obs/templates.json")" != templates ]]; then
  warn "the Templates plugin uses folder '$(jq -r '.folder' "$obs/templates.json")', not templates/"
fi
update_json "$obs/core-plugins.json" '{}' '.templates = true | .bases = true | .properties = true'
# Property types: add the workflow's, keep any the vault already has
update_json "$obs/types.json" '{"types": {}}' \
  ".types = ($(cat "$VAULT_SCAFFOLD/obsidian-types.json")) + (.types // {})"

# --- settings: locations, ticket sources, ... (vault-configure) ---
configure_flags=(--missing)
[[ "$defaults" == 1 ]] && configure_flags+=(--defaults)
[[ "$allow_overlap" == 1 ]] && configure_flags+=(--allow-overlap)
vault-configure "$vault" "${configure_flags[@]}"

# --- what isn't automatic ---
if [[ ! -d "$obs/plugins/obsidian-tasks-plugin" ]]; then
  warn "the Tasks community plugin isn't installed here: add \"$(basename "$vault")\" to programs.claude-tickets.obsidian.vaults (home-manager), or install it from Obsidian"
fi
echo "ct vault init: done. Next (on the default vault; ct vault default to switch): ct sync, ct new, ct start <ID>"
