usage() {
  cat <<'EOF2'
Usage: vault-default [<vault> | --pick | --unset]

Shows or sets the default Obsidian vault: the vault every vault-aware
command acts on (ticket-sync, ticket-start, ticket-new, ticket-feedback,
ticket-status, ticket-attach, ticket-ws, kb, repo-layout, vault-configure,
claude-vault). Switch it to work on another vault. Without one,
they use the only vault under ~/Documents/Obsidian. vault-init always asks.

  vault-default          show the default and where it comes from
  vault-default <vault>  set it: a vault name under ~/Documents/Obsidian
                         (or a path)
  vault-default --pick   choose it from the list of vaults
  vault-default --unset  remove it

The default is stored in ~/.config/claude-tickets/default-vault. Sessions already
running keep the vault they started with.
EOF2
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  "")
    if [[ -n "$launch_vault" ]]; then
      echo "default vault: $launch_vault (CLAUDE_TICKETS_VAULT: the vault this session was started with)"
    elif [[ -s "$default_vault_file" ]]; then
      echo "default vault: $(cat "$default_vault_file") (from $default_vault_file)"
    else
      echo "no default vault set (vault-default <vault> to set one)"
      exit 1
    fi
    default_vault >/dev/null || exit 1
    ;;
  --unset)
    rm -f "$default_vault_file"
    echo "vault-default: default vault removed"
    ;;
  *)
    if [[ "$1" == --pick ]]; then
      vault="$(select_vault "" --no-default)"
    else
      vault="$(select_vault "$1" --no-default)"
    fi
    echo "vault-default: default vault is now $(set_default_vault "$vault")"
    [[ -z "$launch_vault" ]] \
      || warn "this shell has CLAUDE_TICKETS_VAULT=$launch_vault (from a ticket session), which wins over the default here; unset CLAUDE_TICKETS_VAULT"
    ;;
esac
