usage() {
  cat <<'EOF'
Usage: ct kb repo <command> ...   (inside a kb session)

Exploration clones for a kb session: look at any branch or commit of a repo
without touching its main clone under ~/Projects (read-only here). Each is a
shared clone (it borrows the main clone's objects) at
~/work/.kb-<vault>/<slug>, registered in the session's workspace.json, so the
session's code graph swaps it in for the main clone and rebuilds it as the
checkout changes. A <repo> is a slug or <provider>/<owner>/<repo>.

Commands:
  checkout [--force] <repo> [<branch>|<tag>|<commit>]
      Check out a branch (origin's, as a local branch), a tag or a commit,
      creating the exploration clone if needed. Default: the remote's default
      branch. Refuses while the clone has local edits; --force discards them.
  fetch [<repo>...]
      Refresh remote branches and tags: from the main clone (as fetched on
      the host), then from the remote itself where the sandbox can reach it.
      A clean checked-out branch is fast-forwarded. Default: every
      exploration clone.
  graph <repo>
      Rebuild the repo's code graph now (it also rebuilds by itself after a
      checkout or fetch).
  ls
      Exploration clones: what's checked out, and how far behind the remote.
  reset <repo>|--all
      Remove exploration clones; the graph falls back to the main clone.

The clones are the session's own: edit, build and test in them as needed to
check how things behave, but never commit or push (both denied). Edits stay
in the session; checkout --force or reset throws them away. Main clones
under ~/Projects are never edited.
EOF
}

# The kb workspace: the nearest folder up from here whose workspace.json is
# the kb's, so this also works from inside an exploration clone
dir="${KB_DIR:-$PWD}"
while [[ "$(jq -r '.id // empty' "$dir/workspace.json" 2>/dev/null)" != KB ]]; do
  [[ "$dir" != / ]] || die "not in a kb session (no kb workspace.json above $PWD)"
  dir="$(dirname "$dir")"
done
ws="$dir/workspace.json"

clone_of() {  # clone_of <repo> -> sets clone slug main path
  clone="$(resolve_repo "$1")" || die "no main clone '$1' (see ct ws repos)"
  slug="$(repo_slug "$clone")"
  main="$projects_root/$clone"
  path="$dir/$slug"
}

# Remote branches and tags, from the main clone; then from the remote too
refresh() {
  shared_clone_refresh "$main" "$path"
  # Never prompt: no credentials, no host-key questions, just fail fast
  if ! GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND="ssh -o BatchMode=yes -o ConnectTimeout=10" \
       timeout 30 git -C "$path" fetch --quiet --prune origin 2>/dev/null; then
    echo "ct kb repo: $slug: can't reach origin from the sandbox; using the main clone's fetch (run ct ws fetch $slug on the host for newer)" >&2
  fi
}

ensure_clone() {
  if [[ ! -d "$path/.git" ]]; then
    shared_clone "$main" "$path"
  fi
  refresh
}

register() {  # register <ref checked out>
  workspace_put "$ws" "$clone" "$path" "$1" "$1" explore
}

cmd_checkout() {
  local force=0
  if [[ "${1:-}" == --force ]]; then
    force=1
    shift
  fi
  clone_of "${1:?ct kb repo checkout [--force] <repo> [<ref>]}"
  local ref="${2:-}"
  ensure_clone
  if [[ -n "$(git -C "$path" status --porcelain 2>/dev/null)" ]]; then
    [[ "$force" == 1 ]] || die "$slug has local edits; ct kb repo checkout --force $slug ... discards them"
    git -C "$path" reset --quiet --hard
    git -C "$path" clean --quiet -fd -e graphify-out/
  fi
  if [[ -z "$ref" ]]; then
    ref="$(git -C "$path" symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null || true)"
    ref="${ref#origin/}"
    [[ -n "$ref" ]] || ref="$(git -C "$main" rev-parse --abbrev-ref HEAD)"
  fi
  if git -C "$path" rev-parse -q --verify "refs/remotes/origin/$ref" >/dev/null; then
    git -C "$path" checkout --quiet -B "$ref" "origin/$ref"
    git -C "$path" branch --quiet --set-upstream-to "origin/$ref"
  elif git -C "$path" rev-parse -q --verify "$ref^{commit}" >/dev/null; then
    git -C "$path" checkout --quiet --detach "$ref"
  else
    die "no branch, tag or commit '$ref' in $slug (ct kb repo fetch $slug, or ct ws fetch $slug on the host)"
  fi
  register "$ref"
  echo "ct kb repo: $slug at $ref ($(git -C "$path" log -1 --format='%h %cs %s'))"
  echo "ct kb repo: path $path; the code graph picks it up within ~15s"
}

cmd_fetch() {
  local repos=("$@") r up
  if [[ ${#repos[@]} -eq 0 ]]; then
    mapfile -t repos < <(jq -r '.repos[] | select(.mode == "explore") | .slug' "$ws")
  fi
  for r in "${repos[@]}"; do
    clone_of "$r"
    [[ -d "$path/.git" ]] || { warn "$slug has no exploration clone (ct kb repo checkout $slug)"; continue; }
    refresh
    if up="$(git -C "$path" rev-parse --abbrev-ref '@{u}' 2>/dev/null)" \
       && [[ -z "$(git -C "$path" status --porcelain)" ]]; then
      git -C "$path" merge --quiet --ff-only "$up" 2>/dev/null || warn "$slug: can't fast-forward to $up"
    fi
    echo "ct kb repo: $slug fetched ($(git -C "$path" log -1 --format='%h %cs %s'))"
  done
}

cmd_graph() {
  clone_of "${1:?ct kb repo graph <repo>}"
  [[ -d "$path/.git" ]] || die "$slug has no exploration clone (ct kb repo checkout $slug)"
  mkdir -p "$path/graphify-out"
  touch "$path/graphify-out/.rebuild"
  echo "ct kb repo: asked the graph sidecar to rebuild $slug"
}

cmd_ls() {
  {
    printf 'REPO\tAT\tBEHIND\tCOMMIT\n'
    jq -r '.repos[] | select(.mode == "explore") | [.slug, .path, .branch] | @tsv' "$ws" \
      | while IFS=$'\t' read -r s p b; do
          behind="-"
          if git -C "$p" rev-parse -q --verify '@{u}' >/dev/null 2>&1; then
            behind="$(git -C "$p" rev-list --count 'HEAD..@{u}')"
          fi
          printf '%s\t%s\t%s\t%s\n' "$s" "$b" "$behind" "$(git -C "$p" log -1 --format='%h %cs %s' | cut -c1-60)"
        done
  } | column -t -s $'\t'
}

cmd_reset() {
  local targets=() t
  if [[ "${1:-}" == --all ]]; then
    mapfile -t targets < <(jq -r '.repos[] | select(.mode == "explore") | .slug' "$ws")
  else
    targets=("${1:?ct kb repo reset <repo>|--all}")
  fi
  for t in "${targets[@]}"; do
    clone_of "$t"
    rm -rf "$path"
    # shellcheck disable=SC2016 # a jq filter
    json_update "$ws" --arg slug "$slug" '.repos |= map(select(.slug != $slug))'
    echo "ct kb repo: removed $slug's exploration clone"
  done
}

cmd="${1:-}"
shift || true
case "$cmd" in
  checkout) cmd_checkout "$@" ;;
  fetch) cmd_fetch "$@" ;;
  graph) cmd_graph "$@" ;;
  ls) cmd_ls ;;
  reset) cmd_reset "$@" ;;
  -h|--help|help|"") usage ;;
  *) usage >&2; exit 1 ;;
esac
