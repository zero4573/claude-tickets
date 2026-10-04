usage() {
  cat <<'EOF'
Usage: vault-lock acquire <vault> <owner>
       vault-lock release <vault> <owner>
       vault-lock status  <vault>
       vault-lock run     <vault> <owner> -- <command> [args...]

An exclusive lock on a vault's shared notes (projects/, knowledge-base/,
references/), so concurrent ticket sessions running /tickets:save take turns.
<owner> identifies the holder, e.g. the ticket key.

  acquire  waits (up to 10 minutes) until the lock is free, then takes it.
           Re-acquiring as the same owner succeeds. A lock older than 30
           minutes is treated as abandoned and taken over.
  release  frees the lock if <owner> holds it.
  run      acquire, run the command, release.

The lock is the directory <vault>/.vault.lock.d (mkdir is atomic), holding
an `owner` file; stale takeovers serialize on <vault>/.vault.lock.d.guard.
Dot-files are hidden from Obsidian.
EOF
}

wait_seconds=600
stale_seconds=1800

lock_dir() {
  [[ -d "$1/.obsidian" ]] || die "not an Obsidian vault: $1"
  echo "$1/.vault.lock.d"
}

holder() {
  cat "$1/owner" 2>/dev/null || true
}

lock_age() {
  echo $(( $(date +%s) - $(stat -c %Y "$1/owner" 2>/dev/null || date +%s) ))
}

# Replaces an abandoned lock with ours. Two waiters can find it stale at
# once, so the check is repeated under a guard flock: the second one then
# sees the first one's fresh lock and keeps waiting instead of removing it.
take_over() {
  local dir="$1" owner="$2" age
  exec 9>"$dir.guard"
  flock 9
  age="$(lock_age "$dir")"
  if [[ "$age" -gt "$stale_seconds" ]]; then
    warn "taking over a ${age}s-old lock held by '$(holder "$dir")'"
    rm -rf "$dir"
    if mkdir "$dir" 2>/dev/null; then
      echo "$owner" > "$dir/owner"
      flock -u 9
      exec 9>&-
      return 0
    fi
  fi
  flock -u 9
  exec 9>&-
  return 1
}

acquire() {
  local dir owner="$2" waited=0
  dir="$(lock_dir "$1")"
  [[ -n "$owner" ]] || die "acquire: <owner> is required"
  while true; do
    if mkdir "$dir" 2>/dev/null; then
      echo "$owner" > "$dir/owner"
      return 0
    fi
    [[ "$(holder "$dir")" == "$owner" ]] && { touch "$dir/owner"; return 0; }
    if [[ "$(lock_age "$dir")" -gt "$stale_seconds" ]] && take_over "$dir" "$owner"; then
      return 0
    fi
    [[ "$waited" -ge "$wait_seconds" ]] && die "acquire: still locked by '$(holder "$dir")' after ${wait_seconds}s"
    [[ "$waited" == 0 ]] && warn "waiting for the vault lock held by '$(holder "$dir")'..."
    sleep 2
    waited=$((waited + 2))
  done
}

release() {
  local dir owner="$2" current
  dir="$(lock_dir "$1")"
  [[ -d "$dir" ]] || return 0
  current="$(holder "$dir")"
  [[ "$current" == "$owner" ]] || die "release: the lock is held by '$current', not '$owner'"
  rm -rf "$dir"
}

cmd="${1:-}"
shift || true
case "$cmd" in
  acquire) acquire "${1:-}" "${2:-}" ;;
  release) release "${1:-}" "${2:-}" ;;
  status)
    dir="$(lock_dir "${1:-}")"
    if [[ -d "$dir" ]]; then echo "locked by '$(holder "$dir")'"; else echo "unlocked"; fi
    ;;
  run)
    vault="${1:-}" owner="${2:-}"
    [[ "${3:-}" == -- && $# -ge 4 ]] || { usage >&2; exit 1; }
    shift 3
    acquire "$vault" "$owner"
    trap 'release "$vault" "$owner"' EXIT
    "$@"
    ;;
  -h|--help|"") usage ;;
  *) usage >&2; exit 1 ;;
esac
