#!/bin/sh
# Entry point of the ticket graph server (ct graph mcp): keeps one graph
# per worktree of the workspace current and merges them with every main
# clone's graph into $PROJECT_ROOT/graphify-out/graph.json (ticket-merge.py),
# then serves that graph as an MCP server on stdin/stdout. stdout belongs to
# MCP, so everything else goes to stderr. Expects:
#   PROJECT_ROOT   the ticket or kb workspace (read-write)
#   PROJECTS_ROOT  the main clones (read-only)
# Run from the image (env set there) or from its unpacked rootfs (podman
# --rootfs, which doesn't apply the image's env), so it sets its own.
set -eu
export PATH="/opt/graphify/venv/bin:$PATH" GRAPHIFY_QUERY_LOG_DISABLE=1 HOME=/tmp
cd "$PROJECT_ROOT"
mkdir -p graphify-out
python3 /opt/graphify/ticket-merge.py --initial >&2
# The merger is long-lived: restart it if it ever dies, or the graph would
# silently stop following the worktrees
( while true; do python3 /opt/graphify/ticket-merge.py >&2; sleep 5; done ) &
exec python3 -m graphify.serve "$PROJECT_ROOT/graphify-out/graph.json"
