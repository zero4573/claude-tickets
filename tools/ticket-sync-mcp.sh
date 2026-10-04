# --- Minimal MCP client (Streamable HTTP, JSON-RPC) ---
# Talks to one MCP server with no model in the loop. Where each server lives
# comes from $CLAUDE_TICKETS_MCP_CONFIG, a standard MCP config
# ({"mcpServers": {"<name>": {"type": "http", "url": ..., "headers": {...}}}});
# the ticket sources name their server ("mcp" in tickets/.sources.json).
# $CLAUDE_TICKETS_MCP_PREPARE, if set, runs once before the first call
# (e.g. to start a local proxy that holds the credentials).
mcp_url=""
mcp_headers=()
mcp_session=""
mcp_next_id=1
mcp_reply=""
mcp_prepared=0

# mcp_post <json body>: sets mcp_reply to the response's JSON-RPC messages,
# and mcp_session from the Mcp-Session-Id header when the server sends one.
# Globals, not stdout: a $(...) subshell would lose the session.
mcp_post() {
  local resp headers body sid
  local args=(-sS -i --max-time 120 -X POST "$mcp_url"
    -H 'Content-Type: application/json'
    -H 'Accept: application/json, text/event-stream'
    -H 'MCP-Protocol-Version: 2025-06-18')
  [[ -n "$mcp_session" ]] && args+=(-H "Mcp-Session-Id: $mcp_session")
  resp="$(curl "${args[@]}" "${mcp_headers[@]}" --data-binary "$1")" || return 1
  resp="${resp//$'\r'/}"
  headers="${resp%%$'\n\n'*}"
  body="${resp#*$'\n\n'}"
  sid="$(sed -n 's/^[Mm][Cc][Pp]-[Ss]ession-[Ii][Dd]: *//p' <<< "$headers" | head -n1)"
  [[ -n "$sid" ]] && mcp_session="$sid"
  [[ "$headers" =~ ^HTTP/[0-9.]+\ 2 ]] || { warn "MCP: $(head -n1 <<< "$headers") $(head -c 300 <<< "$body")"; return 1; }
  # A streamed response carries the JSON-RPC messages as SSE data lines
  if grep -qi '^content-type: *text/event-stream' <<< "$headers"; then
    mcp_reply="$(sed -n 's/^data: *//p' <<< "$body")"
  else
    mcp_reply="$body"
  fi
}

# mcp_rpc <method> <params json>: prints the JSON-RPC result
mcp_rpc() {
  local id=$((mcp_next_id++)) out
  mcp_post "$(jq -nc --arg m "$1" --argjson p "$2" --argjson id "$id" \
    '{jsonrpc: "2.0", id: $id, method: $m, params: $p}')" || return 1
  out="$(jq -c --argjson id "$id" 'select(.id == $id)' <<< "$mcp_reply" 2>/dev/null | tail -n1)"
  if [[ -z "$out" ]] || jq -e '.error' <<< "$out" >/dev/null; then
    warn "MCP $1: $(jq -c '.error // "no response"' <<< "${out:-null}")"
    return 1
  fi
  jq -c '.result' <<< "$out"
}

# mcp_tool <tool> <arguments json>: prints the tool's JSON payload (the
# Rovo tools return their data as JSON text)
mcp_tool() {
  local result text
  result="$(mcp_rpc tools/call "$(jq -nc --arg n "$1" --argjson a "$2" '{name: $n, arguments: $a}')")" || return 1
  text="$(jq -r '[.content[]? | select(.type == "text") | .text] | join("")' <<< "$result")"
  if jq -e '.isError == true' <<< "$result" >/dev/null; then
    warn "MCP tool $1 failed: $(head -c 500 <<< "$text")"
    return 1
  fi
  jq -c . <<< "$text" 2>/dev/null || { warn "MCP tool $1: not JSON: $(head -c 300 <<< "$text")"; return 1; }
}

# mcp_connect <server>: find the server's URL and open a session
mcp_connect() {
  local server="$1" entry
  [[ -n "${CLAUDE_TICKETS_MCP_CONFIG:-}" ]] \
    || { warn "MCP: CLAUDE_TICKETS_MCP_CONFIG isn't set (a standard MCP config naming '$server'; see the README)"; return 1; }
  entry="$(jq -c --arg s "$server" '.mcpServers[$s] // empty' "$CLAUDE_TICKETS_MCP_CONFIG" 2>/dev/null)"
  [[ -n "$entry" ]] || { warn "MCP: no server '$server' in $CLAUDE_TICKETS_MCP_CONFIG"; return 1; }
  mcp_url="$(jq -r '.url // empty' <<< "$entry")"
  [[ -n "$mcp_url" ]] || { warn "MCP: server '$server' has no url (only HTTP servers are supported)"; return 1; }
  mcp_headers=()
  while IFS= read -r h; do
    [[ -n "$h" ]] && mcp_headers+=(-H "$h")
  done < <(jq -r '.headers // {} | to_entries[] | "\(.key): \(.value)"' <<< "$entry")
  if [[ "$mcp_prepared" == 0 && -n "${CLAUDE_TICKETS_MCP_PREPARE:-}" ]]; then
    mcp_prepared=1
    bash -c "$CLAUDE_TICKETS_MCP_PREPARE" >&2 || { warn "MCP: CLAUDE_TICKETS_MCP_PREPARE failed"; return 1; }
  fi
  mcp_session=""
  mcp_rpc initialize '{"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "ticket-sync", "version": "1"}}' >/dev/null || return 1
  mcp_post '{"jsonrpc": "2.0", "method": "notifications/initialized"}'
}
