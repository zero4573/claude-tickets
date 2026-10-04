usage() {
  cat <<'EOF'
Usage: repo-layout [--apply] [--root <dir>]

Reorganizes the git repos under the current vault's projectsRoot
(~/Projects by default; see vault-default, vault-configure) or --root,
wherever they are nested, into <root>/<provider>/<owner>/<repo>, the
layout the ticket workflow expects. All three come from each repo's
`origin` URL:
  bitbucket  Server/DC (/scm/<key>/<repo>, ssh :7999/<key>/<repo>; owner =
             project key, upper-cased) or Cloud (owner = workspace)
  github     github.com or GitHub Enterprise (owner = user or org)
  gitlab     owner = group, subgroups joined with -
  other      provider = the host name, kebab-cased
The vault and the code graph name each repo by its slug,
<provider>-<owner>-<repo> (ticket-ws repos lists them). Repos without a
hosted origin are left where they are.

Without --apply it only prints the plan. Moving a repo is a rename on the
same filesystem, so uncommitted work is kept. Repos with linked worktrees
are skipped, since moving them breaks the worktrees' links back to the
repo (fix after a manual move with `git worktree repair`), and so are
repos that shared clones in ticket or kb workspaces borrow objects from
(remove those first: ticket-ws rm or gc). Repos are staged in a
temporary folder under the root and then put in place, so nested repos,
and repos sitting where a provider or owner folder must go (e.g. a repo at
~/Projects/bitbucket), are handled. Repos already moved to the older
~/Projects/<key>/<repo> layout are picked up too. Empty folders left
behind are removed.
EOF
}

apply=0
root=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply) apply=1; shift ;;
    --root) root="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 1 ;;
  esac
done
if [[ -z "$root" ]]; then
  require_vault
  root="$projects_root"
fi
[[ -d "$root" ]] || die "no such directory: $root"
root="$(realpath "$root")"

# Prints "<provider>/<owner>/<repo>" for a hosted origin URL, nothing
# otherwise (see remote_identity in tickets-lib.sh)
layout_target() {
  local id
  id="$(remote_identity "$1")"
  [[ -n "$id" ]] || return 0
  tr '\t' / <<< "$id"
}

# Every repo (a directory with a .git directory, so not linked worktrees),
# deepest first so nested repos move before the repos containing them
mapfile -t repos < <(
  find "$root" -mindepth 2 -maxdepth 8 \
    \( -name node_modules -o -name graphify-out -o -name .venv -o -name vendor \) -prune \
    -o -type d -name .git -print 2>/dev/null \
    | sed 's|/\.git$||' | awk '{ print gsub("/", "/"), $0 }' | sort -rn | cut -d' ' -f2-
)

# Plan: for each hosted repo, where it goes
declare -A dest_of=() claimed=()
planned=()
skipped=0

# Main clones that shared clones in ticket and kb workspaces borrow objects
# from (git alternates, see shared_clone): moving one breaks those clones.
# Every vault whose projectsRoot overlaps the root counts, not only the
# current one, since vaults may share their main clones.
declare -A borrowed=()
work_roots=("$work_root")
while IFS= read -r v; do
  IFS=$'\t' read -r w p _ < <(vault_locations "$obsidian_root/$v")
  if paths_overlap "$p" "$root"; then work_roots+=("$w"); fi
done < <(list_vaults 2>/dev/null)
while IFS= read -r alt; do
  while IFS= read -r objects; do
    [[ "$objects" == */.git/objects ]] && borrowed["${objects%/.git/objects}"]=1
  done < "$alt"
done < <(for w in "${work_roots[@]}"; do
  [[ -n "$w" && -d "$w" ]] && find "$w" -maxdepth 7 -path '*/.git/objects/info/alternates' 2>/dev/null
done | sort -u)

# Prints the borrowed repo at or inside a path, if any
borrowed_repo() {
  local b
  for b in "${!borrowed[@]}"; do
    if [[ "$b" == "$1" || "$b" == "$1"/* ]]; then
      echo "$b"
      return 0
    fi
  done
  return 1
}

for repo in "${repos[@]}"; do
  rel="${repo#"$root"/}"
  url="$(git -C "$repo" remote get-url origin 2>/dev/null || true)"
  target="$(layout_target "$url")"
  if [[ -z "$target" ]]; then
    echo "skip   $rel (no hosted origin remote: ${url:-none})"
    skipped=$((skipped + 1))
    continue
  fi
  if [[ "$(git -C "$repo" worktree list --porcelain | grep -c '^worktree ')" -gt 1 ]]; then
    echo "skip   $rel -> $target (has linked worktrees; move manually, then git worktree repair)"
    skipped=$((skipped + 1))
    continue
  fi
  if [[ -n "${claimed[$target]:-}" ]]; then
    echo "skip   $rel -> $target (another clone of it, ${claimed[$target]}, goes there)"
    skipped=$((skipped + 1))
    continue
  fi
  claimed[$target]="$rel"
  dest_of[$repo]="$root/$target"
  planned+=("$repo")
done

# A repo moves if it isn't at its destination yet, or if a repo containing
# it moves (it would be carried along). A repo can also sit where another
# repo's provider or owner folder has to go (e.g. a repo at
# ~/Projects/bitbucket), so every moving repo is first staged outside the
# tree, deepest first, then put in place.
is_moving() {
  local repo="$1" other
  [[ "$repo" != "${dest_of[$repo]}" ]] && return 0
  for other in "${planned[@]}"; do
    [[ "$repo" == "$other"/* && "$other" != "${dest_of[$other]}" ]] && return 0
  done
  return 1
}

# Prints the repo a path is inside of, if that repo isn't moving
inside_staying_repo() {
  local other
  for other in "${repos[@]}"; do
    if [[ "$1" == "$other"/* ]] && ! { [[ -n "${dest_of[$other]:-}" ]] && is_moving "$other"; }; then
      echo "$other"
      return 0
    fi
  done
  return 1
}

# True if the path will be moved out of the way (it is, or is inside, a
# repo that moves)
dest_vacated() {
  local other
  for other in "${planned[@]}"; do
    if [[ "$1" == "$other" || "$1" == "$other"/* ]] && is_moving "$other"; then
      return 0
    fi
  done
  return 1
}

moving=()
for repo in "${planned[@]}"; do
  rel="${repo#"$root"/}"
  target="${dest_of[$repo]#"$root"/}"
  if ! is_moving "$repo"; then
    echo "ok     $rel"
    continue
  fi
  dest="${dest_of[$repo]}"
  if [[ "$repo" != "$dest" && -e "$dest" ]] && ! dest_vacated "$dest"; then
    echo "skip   $rel -> $target (destination exists)"
    skipped=$((skipped + 1))
    continue
  fi
  if [[ "$repo" != "$dest" ]] && staying="$(inside_staying_repo "$dest")"; then
    echo "skip   $rel -> $target (destination is inside the repo ${staying#"$root"/}, which stays)"
    skipped=$((skipped + 1))
    continue
  fi
  if [[ "$repo" != "$dest" ]] && lender="$(borrowed_repo "$repo")"; then
    echo "skip   $rel -> $target (shared clones in ticket or kb workspaces borrow ${lender#"$root"/}'s objects; remove them first with ticket-ws rm / gc)"
    skipped=$((skipped + 1))
    continue
  fi
  dirty=""
  [[ -n "$(git -C "$repo" status --porcelain 2>/dev/null)" ]] && dirty=" (has uncommitted changes, kept)"
  if [[ "$repo" == "$dest" ]]; then
    echo "move   $rel (inside a repo that moves; staged and put back)$dirty"
  else
    echo "move   $rel -> $target$dirty"
  fi
  moving+=("$repo")
done

if [[ "$apply" == 0 ]]; then
  echo "repo-layout: ${#moving[@]} repo(s) to move, $skipped skipped (dry run; --apply to move)"
  exit 0
fi
[[ ${#moving[@]} -gt 0 ]] || { echo "repo-layout: nothing to move"; exit 0; }

# Phase 1: stage (planned is deepest first, so nested repos leave first)
staging="$(mktemp -d "$root/.repo-layout.XXXXXX")"
declare -A staged=()
n=0
for repo in "${moving[@]}"; do
  n=$((n + 1))
  mv "$repo" "$staging/$n"
  staged[$repo]="$staging/$n"
  # Remove folders the move emptied, up to the root
  parent="$(dirname "$repo")"
  while [[ "$parent" == "$root"/* ]] && rmdir "$parent" 2>/dev/null; do
    parent="$(dirname "$parent")"
  done
done

# Phase 2: put each in place (shallowest destination first)
moved=0
while IFS= read -r repo; do
  dest="${dest_of[$repo]}"
  if [[ -e "$dest" ]]; then
    warn "destination $dest exists; left ${repo#"$root"/} at ${staged[$repo]}"
    skipped=$((skipped + 1))
    continue
  fi
  mkdir -p "$(dirname "$dest")"
  mv "${staged[$repo]}" "$dest"
  moved=$((moved + 1))
done < <(for repo in "${moving[@]}"; do
  d="${dest_of[$repo]}"
  echo "$(tr -cd / <<< "$d" | wc -c) $repo"
done | sort -n | cut -d' ' -f2-)
rmdir "$staging" 2>/dev/null || warn "some repos are still staged in $staging"

echo "repo-layout: moved $moved repo(s), skipped $skipped"
