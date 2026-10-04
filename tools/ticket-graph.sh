usage() {
  cat <<'EOF2'
Usage: ticket-graph build [--force]
       ticket-graph status
       ticket-graph mcp <workspace>

The code graph of ticket and kb sessions: one graph merging the workspace's
worktrees with every main clone's graph, served as an MCP server (named
graphify) over stdin/stdout. graphify runs in an image built here, with
podman or docker (CLAUDE_TICKETS_CONTAINER, else detected).

  build    Build localhost/claude-tickets-graphify:<hash> (pinned Python image
           + the hashed graphify lock), and unpack its filesystem into
           ~/.cache/claude-tickets/graphify/rootfs-<hash>. Run on the host;
           ticket-start, kb and graphify-index run it when needed. Only
           rebuilt when the image's sources change (or with --force).
  status   What's built, for which hash.
  mcp      The MCP server for <workspace> (what sessions run). It uses the
           image when the container runtime has it, else the unpacked
           filesystem with `podman run --rootfs`, which works where the
           runtime starts with no images (e.g. podman inside a container).
EOF2
}

# Base image of the graphify image, pinned to a tag and the digest of its
# multi-arch index (UPDATES.md says how to bump). Its Python minor version
# must match the lock's ("# python:" in graphify-requirements.txt).
graph_base_image="docker.io/library/python:3.12.15-slim-trixie@sha256:29113dcae7aad06daa8e95260fa09f27d62be33b9687ea3774f771d601a02256"

# The image sources: $CLAUDE_TICKETS_GRAPH_DIR (set by the package), else
# next to the scripts
graph_src() {
  local d="${CLAUDE_TICKETS_GRAPH_DIR:-}"
  [[ -n "$d" ]] || d="$(config_get graphDir)"
  [[ -n "$d" ]] || d="$(dirname "$(realpath "$0")")/../share/claude-tickets/graph"
  [[ -f "$d/Containerfile" ]] || die "no graphify image sources at $d (set CLAUDE_TICKETS_GRAPH_DIR, or reinstall)"
  echo "$d"
}

# Hash of everything that goes into the image
graph_hash() {
  local src="$1"
  { echo "$graph_base_image"; cat "$src/Containerfile" "$src/graphify-requirements.txt" "$src/serve.sh" "$src/ticket-merge.py"; } \
    | sha256sum | cut -c1-12
}

image_exists() {  # image_exists <runtime> <image>
  if [[ "$1" == podman ]]; then
    podman image exists "$2" 2>/dev/null
  else
    docker image inspect "$2" >/dev/null 2>&1
  fi
}

cmd_build() {
  local force=0 src hash tag rt rootfs tmp cid pull
  [[ "${1:-}" == --force ]] && force=1
  src="$(graph_src)"
  hash="$(graph_hash "$src")"
  tag="localhost/claude-tickets-graphify:$hash"
  rt="$(container_runtime)"
  rootfs="$cache_dir/graphify/rootfs-$hash"
  if [[ "$force" == 1 ]] || ! image_exists "$rt" "$tag"; then
    echo "ticket-graph: building $tag with $rt (installs graphify; takes a minute)" >&2
    # (docker build has no --pull=missing: it pulls a missing base by default)
    pull=()
    [[ "$rt" == podman ]] && pull=(--pull=missing)
    "$rt" build "${pull[@]}" --build-arg "PYTHON_IMAGE=$graph_base_image" \
      -t "$tag" -f "$src/Containerfile" "$src" >&2 \
      || die "building $tag failed"
  fi
  if [[ "$force" == 1 || ! -x "$rootfs/opt/graphify/serve.sh" ]]; then
    echo "ticket-graph: unpacking its filesystem into $rootfs" >&2
    mkdir -p "$cache_dir/graphify"
    tmp="$(mktemp -d "$cache_dir/graphify/.rootfs.XXXXXX")"
    cid="$("$rt" create "$tag" /bin/true)"
    if ! "$rt" export "$cid" | tar --no-same-owner --no-same-permissions -C "$tmp" -xf - 2>/dev/null \
       || [[ ! -x "$tmp/opt/graphify/serve.sh" ]]; then
      "$rt" rm -f "$cid" >/dev/null 2>&1 || true
      rm -rf "$tmp"
      die "unpacking $tag failed"
    fi
    "$rt" rm -f "$cid" >/dev/null 2>&1 || true
    rm -rf "$rootfs"
    mv "$tmp" "$rootfs"
  fi
  # Other hashes' unpacked filesystems, untouched for 30 days
  touch "$rootfs"
  find "$cache_dir/graphify" -mindepth 1 -maxdepth 1 -type d -name 'rootfs-*' ! -path "$rootfs" -mtime +30 \
    -exec rm -rf {} + 2>/dev/null || true
  echo "$tag"
}

cmd_status() {
  local src hash rt
  src="$(graph_src)"
  hash="$(graph_hash "$src")"
  echo "image:   localhost/claude-tickets-graphify:$hash"
  if rt="$(container_runtime 2>/dev/null)"; then
    if image_exists "$rt" "localhost/claude-tickets-graphify:$hash"; then
      echo "         built ($rt)"
    else
      echo "         not built ($rt): ticket-graph build"
    fi
  else
    echo "         no container runtime"
  fi
  if [[ -x "$cache_dir/graphify/rootfs-$hash/opt/graphify/serve.sh" ]]; then
    echo "rootfs:  $cache_dir/graphify/rootfs-$hash"
  else
    echo "rootfs:  not unpacked: ticket-graph build"
  fi
}

cmd_mcp() {
  local ws="${1:-}" src hash tag rt rootfs mounts
  [[ -n "$ws" && -f "$ws/workspace.json" ]] || die "mcp: <workspace> must be a ticket or kb workspace (with workspace.json)"
  ws="$(realpath "$ws")"
  src="$(graph_src)"
  hash="$(graph_hash "$src")"
  tag="localhost/claude-tickets-graphify:$hash"
  rootfs="$cache_dir/graphify/rootfs-$hash"
  mounts=(-v "$ws:$ws" -e "PROJECT_ROOT=$ws" -e "PROJECTS_ROOT=$projects_root")
  [[ -d "$projects_root" ]] && mounts+=(-v "$projects_root:$projects_root:ro")
  if rt="$(container_runtime 2>/dev/null)" && image_exists "$rt" "$tag"; then
    container_run -i "${mounts[@]}" "$tag" /opt/graphify/serve.sh
    exit $?
  elif [[ -x "$rootfs/opt/graphify/serve.sh" ]] && command -v podman >/dev/null; then
    # --rootfs takes the path where the image would go: options before it
    exec podman run --rm -i --security-opt label=disable "${mounts[@]}" \
      --rootfs "$rootfs:O" /opt/graphify/serve.sh
  fi
  die "mcp: the graphify image isn't built (run ticket-graph build on the host)"
}

case "${1:-}" in
  build) shift; cmd_build "$@" ;;
  status) cmd_status ;;
  mcp) shift; require_vault; cmd_mcp "$@" ;;
  -h|--help) usage ;;
  *) usage >&2; exit 1 ;;
esac
