usage() {
  cat <<'EOF2'
Usage: claude-vault [claude args...]

Runs claude in the current repo with the current vault (vault-default) as
its knowledge base: the vault is added (--add-dir), the ticket plugin's
skills are loaded (/tickets:kb, /tickets:recall, /tickets:save), and the
system prompt names the vault and this repo's notes in it. The arguments
are claude's own. Without claude-vault a session knows nothing of the vault.
EOF2
}
[[ "${1:-}" == -h || "${1:-}" == --help ]] && { usage; exit 0; }
dir="$(realpath "$PWD")"

require_vault
[[ -f "$vault/AGENTS.md" ]] || die "$vault has no AGENTS.md (set it up with vault-init)"

# This repo's slug: from its path under ~/Projects, else from its origin
slug=""
if top="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)"; then
  rel="${top#"$projects_root"/}"
  if [[ "$rel" != "$top" && "$rel" == */*/* && "$rel" != */*/*/* ]]; then
    slug="$(repo_slug "$rel")"
  elif id="$(remote_identity "$(git -C "$top" remote get-url origin 2>/dev/null || true)")" && [[ -n "$id" ]]; then
    slug="$(repo_slug "$(tr '\t' / <<< "$id")")"
  fi
fi

prompt="$(
cat <<EOF
# Knowledge base

This session's knowledge base is the Obsidian vault at \`$vault\` (added to
this session, read-write). Read its \`AGENTS.md\` before writing anything there.
EOF
if [[ -n "$slug" ]]; then
  if [[ -f "$vault/projects/$slug/$slug.md" ]]; then
    echo "- This repo is \`$slug\`. Its notes are in \`projects/$slug/\`: the index \`$slug.md\`, \`architecture/$slug-decisions.md\`, \`features/\`, \`sequences/\`, \`logs/\`."
  else
    echo "- This repo is \`$slug\`. It has no notes yet; \`/tickets:save\` creates \`projects/$slug/\`."
  fi
fi
cat <<'EOF'
- System-wide notes are in `projects/system/` (service map, compatibility matrix, flows across services), alongside `knowledge-base/` and `references/`. `tickets/` holds the history of past work.
- For questions about this repo or the wider system, and before non-trivial changes, use the `kb` skill. Check the vault first, then the code graph, then the code.
- Don't edit `projects/`, `knowledge-base/` or `references/` directly. Record what you learn as drafts in `inbox/` (see the `kb` skill); `/tickets:save` promotes them at the end of the session.
- `/tickets:recall` loads this repo's recent history and decisions. Run `/tickets:save` before ending the session.
EOF
)"

session=()
claude_session_cmd session "$vault"
exec "${session[@]}" --append-system-prompt "$prompt" "$@"
