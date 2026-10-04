usage() {
  cat <<'EOF'
Usage: ticket-ws <command> ...

Manages the git worktrees of the current vault's (vault-default) ticket
workspaces (~/work/<ID>; ~/work stands for the vault's workRoot, ~/Projects
for its projectsRoot). Main clones live at
~/Projects/<provider>/<owner>/<repo>, and each has a slug,
<provider>-<owner>-<repo> (e.g. bitbucket-acme-billing-service), which
is how the vault and the code graph name it. A ticket's worktree of a repo
is ~/work/<ID>/<slug>, on branch feature/<ID>[-<description>]. Each
workspace's repos are recorded in ~/work/<ID>/workspace.json.

A <repo> argument is a slug or <provider>/<owner>/<repo>.

Commands:
  repos
      List the main clones: slug and <provider>/<owner>/<repo>.
  clone <provider>/<owner>/<repo> | --url <git url>
      Clone a repo into ~/Projects/<provider>/<owner>/<repo> and build its
      code graph. Run it on the host: the sandbox has no git credentials. The
      default URL is SSH (git@bitbucket.org:<owner>/<repo>.git, or the GitHub
      equivalent). A running ticket session can add the new repo straight
      away: ticket-ws add makes a shared clone when it can't write to the main
      clone's .git.
  add <ID> <repo> --base <branch> [--desc <description>] [--version <target>]
      [--branch <existing branch>]
      Create (or reuse) the worktree, branched from origin/<base> (or the
      local <base> when there's no such remote branch). The branch is reused
      if it already exists. Copies untracked files matching the main clone's
      .worktreeinclude (gitignore syntax, e.g. .env) and seeds graphify-out/
      from the main clone so the code graph builds incrementally.
      --branch checks out an existing branch instead (local, or origin's,
      tracked), e.g. the source branch of an open PR; --base is then the
      branch it merges into.
  ls [<ID>]
      Worktrees per ticket: branch, uncommitted changes, commits ahead of
      and behind the base.
  diff <ID> [--stat]
      Everything changed on the ticket's branches since they left their
      base, committed or not, plus untracked files, per repo.
  rm <ID> [--force]
      Remove the ticket's worktrees (refusing dirty ones unless --force) and
      its workspace files. Branches are kept. A shared clone (see clone) is
      deleted, and refused while it has commits that aren't on origin.
  sign <ID>
      On the host: sign the ticket's commits. Sessions commit on the ticket
      branches but can't sign (no SSH agent in the sandbox), so this replays
      each worktree's unpushed commits onto the same parent with --gpg-sign
      (the repo's per-remote identity key). Same changes and authors, new
      hashes; commits already on a remote are left alone. Run it before
      pushing.
  fetch [<repo>...]
      Fetch origin in the given main clones (default: all), in parallel, then
      pass the new refs on to the shared clones in the vault's ticket and kb
      workspaces. Other vaults' shared clones get them on their own fetch.
  gc [--days N] [--dry-run]
      Free disk in the current vault: remove workspaces of tickets that are
      done or closed and idle for N days (default 14; dirty or unpushed work
      is kept), merged graphs of sessions that aren't running, idle kb
      exploration clones, and graphify's dated snapshots older than a week.
EOF
}

workspace_file() {
  echo "$work_root/$1/workspace.json"
}

# Fields of each repo entry, tab-separated: slug clone path base branch
# (clone = <provider>/<owner>/<repo>)
workspace_repos() {
  local file
  file="$(workspace_file "$1")"
  [[ -f "$file" ]] || return 0
  jq -r '.repos[] | [.slug, "\(.provider)/\(.owner)/\(.repo)", .path, .base, .branch] | @tsv' "$file"
}

# The ref a ticket branch is compared against: origin/<base> if present
base_ref() {
  local dir="$1" base="$2"
  if git -C "$dir" rev-parse -q --verify "refs/remotes/origin/$base" >/dev/null; then
    echo "origin/$base"
  else
    echo "$base"
  fi
}

cmd_repos() {
  local r
  {
    printf 'SLUG\tREPO\n'
    while IFS= read -r r; do
      printf '%s\t%s\n' "$(repo_slug "$r")" "$r"
    done < <(list_main_clones)
  } | column -t -s $'\t'
}

cmd_add() {
  local key="${1:-}" spec="${2:-}" base="" desc="" version="" use_branch=""
  shift $(( $# < 2 ? $# : 2 ))
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --base) base="$(need_val "$@")"; shift 2 ;;
      --desc) desc="$(need_val "$@")"; shift 2 ;;
      --version) version="$(need_val "$@")"; shift 2 ;;
      --branch) use_branch="$(need_val "$@")"; shift 2 ;;
      *) die "add: unknown argument: $1" ;;
    esac
  done
  valid_key "$key" || die "add: not a ticket ID: '$key'"
  [[ -n "$spec" ]] || die "add: <repo> (slug or <provider>/<owner>/<repo>) is required"
  [[ -n "$base" ]] || die "add: --base <branch> is required"
  local clone slug
  clone="$(resolve_repo "$spec")" || die "add: no main clone '$spec' (see ticket-ws repos)"
  slug="$(repo_slug "$clone")"
  local main="$projects_root/$clone" wt="$work_root/$key/$slug" mode=""

  local branch="feature/$key"
  [[ -n "$desc" ]] && branch="$branch-$(kebab "$desc")"
  [[ -n "$use_branch" ]] && branch="$use_branch"

  ensure_workspace "$work_root/$key" "$key"
  if [[ -e "$wt/.git" ]]; then
    branch="$(git -C "$wt" rev-parse --abbrev-ref HEAD)"
    if [[ -d "$wt/.git" ]]; then mode=clone; else mode=worktree; fi
    echo "ticket-ws: $wt already exists (branch $branch)"
  else
    # Best effort: a session may have no git credentials (e.g. sandboxed), so
    # this may only work on the host (ticket-start fetches before launching)
    timeout 30 git -C "$main" fetch --quiet origin "$base" 2>/dev/null \
      || warn "couldn't fetch origin/$base in $slug, using what's already fetched"
    local start
    start="$(base_ref "$main" "$base")"
    git -C "$main" rev-parse -q --verify "$start^{commit}" >/dev/null \
      || die "add: base branch '$base' not found in $slug"
    mkdir -p "$(dirname "$wt")"
    if [[ -z "$use_branch" && -z "$desc" ]]; then
      local existing
      existing="$(git -C "$main" for-each-ref --format='%(refname:short)' "refs/heads/feature/$key" "refs/heads/feature/$key-*" | head -n1)"
      [[ -z "$existing" ]] || branch="$existing"
    fi
    # What the branch starts from: an existing local branch, an existing
    # remote one (e.g. an open PR's source), or a new branch off the base.
    # Upstreams aren't set here (that writes the main clone's .git/config,
    # which sandboxes mount read-only); push.autoSetupRemote covers the push.
    local from=new commit
    if git -C "$main" rev-parse -q --verify "refs/heads/$branch" >/dev/null; then
      from=local
      commit="$(git -C "$main" rev-parse "refs/heads/$branch")"
    elif [[ -n "$use_branch" ]]; then
      git -C "$main" rev-parse -q --verify "refs/remotes/origin/$branch" >/dev/null \
        || die "add: branch '$branch' not found locally or on origin in $slug (fetch on the host: ticket-ws fetch $slug)"
      from=remote
      commit="$(git -C "$main" rev-parse "refs/remotes/origin/$branch")"
    else
      commit="$(git -C "$main" rev-parse "$start^{commit}")"
    fi

    # CLAUDE_TICKETS_WS_SHARED_CLONE=1 forces a shared clone (testing)
    if [[ -w "$main/.git/refs" && -z "${CLAUDE_TICKETS_WS_SHARED_CLONE:-}" ]]; then
      mode=worktree
      if [[ "$from" == local ]]; then
        git -C "$main" worktree add --quiet "$wt" "$branch"
      else
        git -C "$main" worktree add --quiet --no-track -b "$branch" "$wt" "$commit"
      fi
    else
      # The main clone's .git is read-only here: a repo cloned on the host
      # after this ticket's sandbox started isn't in its read-write mounts.
      # A shared clone writes nothing to the main clone, and its origin is
      # the real remote, so the user's push from it goes to the server.
      mode=clone
      shared_clone "$main" "$wt"
      git -C "$wt" checkout --quiet --no-track -B "$branch" "$commit"
      if git -C "$wt" rev-parse -q --verify "refs/remotes/origin/$branch" >/dev/null; then
        git -C "$wt" branch --quiet --set-upstream-to "origin/$branch"
      fi
    fi
    case "$from" in
      new) echo "ticket-ws: created $wt ($mode) on $branch (from $start)" ;;
      *) echo "ticket-ws: created $wt ($mode) on existing branch $branch" ;;
    esac

    # Untracked, ignored files the worktree needs (.env, local config)
    if [[ -f "$main/.worktreeinclude" ]]; then
      (cd "$main" && git ls-files -z --others --ignored --exclude-from=.worktreeinclude) \
        | while IFS= read -r -d '' f; do
            mkdir -p "$wt/$(dirname "$f")"
            cp -p "$main/$f" "$wt/$f"
          done
    fi
    seed_graph "$main" "$wt"
  fi

  workspace_put "$(workspace_file "$key")" "$clone" "$wt" "$base" "$branch" "$mode" "$version"
}

cmd_ls() {
  local keys=() key slug clone path base branch dirty counts ahead behind ref
  if [[ -n "${1:-}" ]]; then
    keys=("$1")
  else
    for d in "$work_root"/*/workspace.json; do
      if [[ -f "$d" ]]; then
        keys+=("$(basename "$(dirname "$d")")")
      fi
    done
  fi
  {
    printf 'TICKET\tREPO\tBRANCH\tBASE\tCHANGED\tAHEAD\tBEHIND\n'
    for key in "${keys[@]}"; do
      while IFS=$'\t' read -r slug clone path base branch; do
        if [[ ! -e "$path/.git" ]]; then
          printf '%s\t%s\t%s\t%s\tmissing\t-\t-\n' "$key" "$slug" "$branch" "$base"
          continue
        fi
        dirty="$(git -C "$path" status --porcelain | wc -l)"
        ref="$(base_ref "$path" "$base")"
        counts="$(git -C "$path" rev-list --left-right --count "$ref...HEAD" 2>/dev/null || echo "? ?")"
        behind="${counts%%[[:space:]]*}"
        ahead="${counts##*[[:space:]]}"
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$key" "$slug" "$branch" "$base" "$dirty" "$ahead" "$behind"
      done < <(workspace_repos "$key")
    done
  } | column -t -s $'\t'
}

cmd_diff() {
  local key="${1:-}" stat=0 slug clone path base branch ref mb untracked
  valid_key "$key" || die "diff: not a ticket ID: '$key'"
  [[ "${2:-}" == --stat ]] && stat=1
  while IFS=$'\t' read -r slug clone path base branch; do
    echo "=== $slug ($branch vs $base) ==="
    [[ -e "$path/.git" ]] || { echo "(worktree missing: $path)"; continue; }
    ref="$(base_ref "$path" "$base")"
    mb="$(git -C "$path" merge-base "$ref" HEAD 2>/dev/null || echo HEAD)"
    if [[ "$stat" == 1 ]]; then
      git -C "$path" --no-pager diff --stat "$mb"
    else
      git -C "$path" --no-pager diff "$mb"
    fi
    untracked="$(git -C "$path" ls-files --others --exclude-standard)"
    if [[ -n "$untracked" ]]; then
      echo "--- untracked files:"
      echo "$untracked"
    fi
    echo
  done < <(workspace_repos "$key")
}

cmd_rm() {
  local key="${1:-}" force=0 slug clone path base branch main failed=0
  valid_key "$key" || die "rm: not a ticket ID: '$key'"
  [[ "${2:-}" == --force ]] && force=1
  local dir="$work_root/$key"
  [[ -d "$dir" ]] || die "rm: no workspace at $dir"
  while IFS=$'\t' read -r slug clone path base branch; do
    main="$projects_root/$clone"
    if [[ -e "$path/.git" ]]; then
      if [[ "$force" == 0 && -n "$(git -C "$path" status --porcelain --ignore-submodules)" ]]; then
        warn "$slug has uncommitted changes, not removing (use --force)"
        failed=1
        continue
      fi
      if [[ -d "$path/.git" ]]; then
        # A shared clone (see add): its branch lives only in it, so refuse to
        # drop commits that were never pushed
        if [[ "$force" == 0 && "$(git -C "$path" rev-list --count HEAD --not --remotes 2>/dev/null || echo 0)" != 0 ]]; then
          warn "$slug has commits that aren't on origin, not removing (push them, or use --force)"
          failed=1
          continue
        fi
        rm -rf "$path"
        echo "ticket-ws: removed $path (shared clone)"
        continue
      fi
      # graphify-out is ignored, so `git worktree remove` would refuse it
      rm -rf "$path/graphify-out"
      if [[ "$force" == 1 ]]; then
        git -C "$main" worktree remove --force "$path"
      else
        git -C "$main" worktree remove "$path"
      fi
      echo "ticket-ws: removed $path (branch $branch kept)"
    fi
    git -C "$main" worktree prune 2>/dev/null || true
  done < <(workspace_repos "$key")
  [[ "$failed" == 0 ]] || die "rm: some worktrees were kept; workspace $dir left in place"
  rm -rf "$dir/graphify-out" "$dir/.claude"
  rm -f "$dir/workspace.json" "$dir/workspace.json.lock" "$dir/CLAUDE.md" "$dir/.agent-state" \
    "$dir/$key.code-workspace"
  find "$dir" -depth -type d -empty -delete 2>/dev/null || true
  if [[ -d "$dir" ]]; then
    warn "left $dir in place: it still has other files"
  else
    echo "ticket-ws: removed workspace $dir"
  fi
}

# Clones a repo into $projects_root/<provider>/<owner>/<repo> on the host (the
# sandbox has no git credentials), then builds its code graph
cmd_clone() {
  local spec="${1:-}" url="" id clone dest
  shift || true
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --url) url="${2:-}"; shift 2 ;;
      *) die "clone: unknown argument: $1" ;;
    esac
  done
  if [[ -z "$url" ]]; then
    [[ "$spec" == */*/* ]] || die "clone: give <provider>/<owner>/<repo> (or --url <git url>)"
    local provider="${spec%%/*}" rest="${spec#*/}"
    case "$provider" in
      bitbucket) url="git@${CLAUDE_TICKETS_BITBUCKET_HOST:-bitbucket.org}:$rest.git" ;;
      github) url="git@github.com:$rest.git" ;;
      *) die "clone: no default URL for provider '$provider'; pass --url <git url>" ;;
    esac
  fi
  id="$(remote_identity "$url")"
  [[ -n "$id" ]] || die "clone: can't tell provider/owner/repo from '$url'"
  clone="$(tr '\t' / <<< "$id")"
  dest="$projects_root/$clone"
  if [[ -d "$dest/.git" ]]; then
    echo "ticket-ws: $clone is already cloned at $dest"
    return 0
  fi
  mkdir -p "$(dirname "$dest")"
  git clone "$url" "$dest" \
    || die "clone: git clone failed (a session may have no git credentials: run this on the host)"
  echo "ticket-ws: cloned $clone (slug $(repo_slug "$clone"))"
  if command -v graphify-index >/dev/null 2>&1; then
    graphify-index "$clone" || warn "graphify-index failed for $clone; run it again later"
  fi
}

# Signs a ticket's commits on the host: the sandbox commits unsigned (it has
# no SSH agent), so each worktree's commits that no remote has yet are
# replayed onto the same parent with --gpg-sign (the key the repo's
# per-remote git identity sets). Same trees and authors, new hashes; pushed
# commits are never touched.
cmd_sign() {
  local key="${1:-}" slug clone path base branch first parent n unsigned failed=0 onto=()
  valid_key "$key" || die "sign: not a ticket ID: '$key'"
  [[ ! -e /run/.containerenv && ! -e /.dockerenv ]] || die "sign: run it on the host, where your signing key is"
  [[ -f "$(workspace_file "$key")" ]] || die "sign: $key has no workspace ($work_root/$key)"
  while IFS=$'\t' read -r slug clone path base branch; do
    [[ -e "$path/.git" ]] || { warn "sign: $slug: worktree missing ($path)"; continue; }
    if [[ -z "$(git -C "$path" rev-list -n1 HEAD --not --remotes)" ]]; then
      echo "ticket-ws: $slug: no unpushed commits on $branch"
      continue
    fi
    # From the oldest unpushed commit without a signature (%G? N); the ones
    # before it are already signed and keep their hashes
    first="$(git -C "$path" log --reverse --topo-order --no-show-signature --format='%H %G?' \
      HEAD --not --remotes | awk '$2 == "N" { print $1; exit }')"
    if [[ -z "$first" ]]; then
      echo "ticket-ws: $slug: unpushed commits on $branch are already signed"
      continue
    fi
    n="$(git -C "$path" rev-list --count HEAD "^$first^" --not --remotes 2>/dev/null \
      || git -C "$path" rev-list --count HEAD --not --remotes)"
    if parent="$(git -C "$path" rev-parse -q --verify "$first^")"; then
      onto=("$parent")
    else
      onto=(--root)
    fi
    if ! git -C "$path" rebase --quiet --force-rebase --rebase-merges --autostash \
         --gpg-sign "${onto[@]}"; then
      git -C "$path" rebase --abort 2>/dev/null || true
      warn "sign: $slug: couldn't re-sign $branch (see above); left as it was"
      failed=1
      continue
    fi
    unsigned="$(git -C "$path" log --no-show-signature --format=%G? HEAD --not --remotes | grep -c '^N$' || true)"
    if [[ "$unsigned" -gt 0 ]]; then
      warn "sign: $slug: $unsigned of $n commit(s) on $branch still unsigned (no signing key for this repo's identity?)"
      failed=1
    else
      echo "ticket-ws: $slug: signed $n commit(s) on $branch"
    fi
  done < <(workspace_repos "$key")
  [[ "$failed" == 0 ]] || exit 1
}

cmd_fetch() {
  local repos=() spec clone
  for spec in "$@"; do
    clone="$(resolve_repo "$spec")" || die "fetch: no main clone '$spec' (see ticket-ws repos)"
    repos+=("$clone")
  done
  [[ ${#repos[@]} -gt 0 ]] || mapfile -t repos < <(list_main_clones)
  [[ ${#repos[@]} -gt 0 ]] || return 0
  echo "ticket-ws: fetching ${#repos[@]} repo(s)..." >&2
  # shellcheck disable=SC2016 # expanded by the inner sh
  printf '%s\n' "${repos[@]}" | xargs -P 8 -I{} sh -c '
    timeout 60 git -C "$1/$2" fetch --quiet --prune origin 2>/dev/null || echo "ticket-ws: fetch failed: $2" >&2
  ' _ "$projects_root" {}
  # Shared clones (ticket and kb workspaces) copy their refs from the main
  # clone, so pass the new ones on
  local ws path main
  for ws in "$work_root"/*/workspace.json "$work_root"/.kb-*/workspace.json; do
    [[ -f "$ws" ]] || continue
    while IFS=$'\t' read -r path main; do
      [[ -d "$path/.git" && -d "$projects_root/$main/.git" ]] || continue
      shared_clone_refresh "$projects_root/$main" "$path" 2>/dev/null \
        || warn "couldn't refresh $path from its main clone"
    done < <(jq -r '.repos[] | select(.mode == "clone" or .mode == "explore")
                  | [.path, "\(.provider)/\(.owner)/\(.repo)"] | @tsv' "$ws")
  done
}

# Disk clean-up for everything the workflow leaves behind:
#  * workspaces of tickets that are done or closed, idle for --days (via
#    cmd_rm, which keeps dirty worktrees and unpushed commits)
#  * merged session graphs (rebuilt on start) of workspaces with no running session
#  * kb exploration clones with no edits, idle for --days
#  * graphify's dated snapshot folders older than a week, everywhere
cmd_gc() {
  local days=14 dry=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --days) days="$(need_val "$@")"; shift 2 ;;
      --dry-run|-n) dry=1; shift ;;
      *) die "gc: unknown argument: $1" ;;
    esac
  done
  [[ "$days" =~ ^[0-9]+$ ]] || die "gc: --days must be a number"
  # It tells running sessions apart by the vault's tmux session; inside a
  # container it would see none and clean up live workspaces
  [[ ! -e /run/.containerenv && ! -e /.dockerenv ]] || die "gc: run it on the host, not inside a container"
  local act=""
  [[ "$dry" == 1 ]] && act="would "
  local ws dir key vault status running
  # Workspaces kept although their ticket is finished, per vault: tasks for
  # a follow-up note in that vault's inbox/
  local -A kept=()

  idle() {  # idle <path>: untouched for --days
    [[ -n "$(find "$1" -maxdepth 0 -mtime +"$days" 2>/dev/null)" ]]
  }

  for ws in "$work_root"/*/workspace.json "$work_root"/.kb-*/workspace.json; do
    [[ -f "$ws" ]] || continue
    dir="$(dirname "$ws")"
    key="$(basename "$dir")"
    running=0
    session_running "$dir" && running=1

    # Finished tickets
    if [[ "$key" != .kb-* && "$running" == 0 ]] && idle "$ws"; then
      vault="$(jq -r '.vault // empty' "$ws")"
      status="$([[ -n "$vault" ]] && frontmatter_get "$(ticket_note "$vault" "$key")" status)"
      if [[ "$status" == "done" || "$status" == "closed" ]]; then
        echo "ticket-ws gc: ${act}remove $key ($status)"
        if [[ "$dry" == 0 ]]; then
          if ! (cmd_rm "$key"); then
            warn "gc: kept $key (see above)"
            [[ -z "$vault" ]] || kept[$vault]+="Clean up the workspace of [[$key]] ($status): $dir has uncommitted or unpushed work. Review it with ticket-ws diff $key, then ticket-ws rm $key (--force to drop it)"$'\n'
          fi
          [[ -d "$dir" ]] || continue
        fi
      fi
    fi

    # Merged graph of a session that isn't running
    if [[ "$running" == 0 && -d "$dir/graphify-out" ]]; then
      echo "ticket-ws gc: ${act}drop the merged graph of $key"
      [[ "$dry" == 1 ]] || rm -rf "$dir/graphify-out"
    fi

    # Idle kb exploration clones
    if [[ "$key" == .kb-* && "$running" == 0 ]]; then
      local slug path
      while IFS=$'\t' read -r slug path; do
        [[ -d "$path/.git" ]] || continue
        if idle "$path/.git/HEAD" && [[ -z "$(git -C "$path" status --porcelain 2>/dev/null)" ]]; then
          echo "ticket-ws gc: ${act}remove kb exploration clone $slug"
          if [[ "$dry" == 0 ]]; then
            rm -rf "$path"
            # shellcheck disable=SC2016 # a jq filter
            json_update "$ws" --arg slug "$slug" '.repos |= map(select(.slug != $slug))'
          fi
        fi
      done < <(jq -r '.repos[] | select(.mode == "explore") | [.slug, .path] | @tsv' "$ws")
    fi
  done

  # graphify's dated snapshots: main clones and checkouts
  if [[ "$dry" == 0 ]]; then
    local outs=()
    mapfile -t outs < <(
      for d in "$projects_root"/*/*/*/graphify-out "$work_root"/*/*/graphify-out "$work_root"/.kb-*/*/graphify-out; do
        [[ -d "$d" ]] && echo "$d"
      done
    )
    prune_graph_snapshots "${outs[@]}"
    echo "ticket-ws gc: pruned graphify snapshots older than a week in ${#outs[@]} graph folder(s)"
  fi

  local v tasks note
  for v in "${!kept[@]}"; do
    tasks="$(mktemp)"
    printf '%s' "${kept[$v]}" > "$tasks"
    note="$(followup_note "$v" ticket-ws-gc "ticket-ws gc" "$tasks")"
    rm -f "$tasks"
    [[ -z "$note" ]] || echo "ticket-ws gc: follow-ups in $note"
  done
}

cmd="${1:-}"
shift || true
# Everything but repos and clone works in the current vault's workRoot
case "$cmd" in
  add|ls|diff|rm|sign|fetch|gc) require_vault ;;
esac
case "$cmd" in
  repos) cmd_repos "$@" ;;
  clone) cmd_clone "$@" ;;
  add) cmd_add "$@" ;;
  ls) cmd_ls "$@" ;;
  diff) cmd_diff "$@" ;;
  rm) cmd_rm "$@" ;;
  sign) cmd_sign "$@" ;;
  fetch) cmd_fetch "$@" ;;
  gc) cmd_gc "$@" ;;
  -h|--help|help|"") usage ;;
  *) usage >&2; exit 1 ;;
esac
