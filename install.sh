#!/usr/bin/env bash
# Installs claude-tickets without Nix: ./install.sh [--prefix <dir>]
#   <prefix>/bin                         the commands (tickets-lib.sh prepended
#                                        to each, as the Nix package does)
#   <prefix>/share/claude-tickets/       plugin/, graph/, vault-scaffold/
#   <prefix>/share/zsh/site-functions/   _tickets (zsh completion)
# and records the plugin and graph dirs, and the container runtime it finds,
# in ~/.config/claude-tickets/config.json.
#   --prefix <dir>  install location (default ~/.local)
#   --check         only check the dependencies
#   --uninstall     remove what a previous install put under <prefix>
set -euo pipefail
cd "$(dirname "$0")"

prefix="$HOME/.local" mode=install
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) prefix="${2:?--prefix needs a directory}"; shift 2 ;;
    --check) mode=check; shift ;;
    --uninstall) mode=uninstall; shift ;;
    -h|--help) sed -n '2,11s/^# \{0,1\}//p' "$0"; exit 0 ;;
    *) echo "install.sh: unknown argument: $1" >&2; exit 1 ;;
  esac
done
share="$prefix/share/claude-tickets"
manifest="$share/.installed"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/claude-tickets"

if [[ "$mode" == uninstall ]]; then
  [[ -f "$manifest" ]] || { echo "install.sh: nothing installed under $prefix" >&2; exit 1; }
  while IFS= read -r f; do rm -f "$prefix/$f"; done < "$manifest"
  rm -rf "$share"
  echo "install.sh: removed claude-tickets from $prefix (your vaults, workspaces and $config_dir are untouched)"
  exit 0
fi

# --- dependencies ---
darwin=0
[[ "$(uname -s)" == Darwin ]] && darwin=1
missing=()
have() { command -v "$1" >/dev/null 2>&1; }
gnu() {  # gnu <tool>: the GNU one, g-prefixed on macOS
  if [[ "$darwin" == 1 ]]; then have "g$1"; else have "$1"; fi
}
(( BASH_VERSINFO[0] >= 4 )) || missing+=("bash 4+ (this is $BASH_VERSION)")
for t in git jq curl tmux fzf flock python3; do have "$t" || missing+=("$t"); done
have gawk || missing+=(gawk)
for t in realpath sed find; do gnu "$t" || missing+=("GNU $t"); done
have podman || have docker || missing+=("podman or docker (for the code graph)")
have claude || missing+=("claude (Claude Code)")
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "install.sh: missing: ${missing[*]}" >&2
  [[ "$darwin" == 1 ]] && echo "  (macOS: brew install bash coreutils gnu-sed findutils gawk flock jq tmux fzf)" >&2
  [[ "$mode" == check ]] && exit 1
  echo "install.sh: installing anyway; the commands that need them will fail until they're there" >&2
fi
[[ "$mode" == check ]] && { echo "install.sh: all dependencies found"; exit 0; }

# --- files ---
mkdir -p "$prefix/bin" "$share" "$prefix/share/zsh/site-functions"
: > "$manifest.tmp"
lib="tools/tickets-lib.sh"
for src in tools/*.sh; do
  name="$(basename "$src" .sh)"
  case "$name" in tickets-lib|ticket-sync-mcp|ticket-sync-jira) continue ;; esac
  out="$prefix/bin/$name"
  {
    echo '#!/usr/bin/env bash'
    echo 'set -euo pipefail'
    echo "export CLAUDE_TICKETS_PLUGIN=\"\${CLAUDE_TICKETS_PLUGIN:-$share/plugin}\""
    echo "export CLAUDE_TICKETS_GRAPH_DIR=\"\${CLAUDE_TICKETS_GRAPH_DIR:-$share/graph}\""
    [[ "$name" == vault-init ]] && echo "export VAULT_SCAFFOLD=\"$share/vault-scaffold\""
    if [[ "$name" == ticket-sync ]]; then cat tools/ticket-sync-mcp.sh tools/ticket-sync-jira.sh; fi
    cat "$lib" "$src"
  } > "$out"
  chmod 0755 "$out"
  echo "bin/$name" >> "$manifest.tmp"
done
{ echo '#!/usr/bin/env python3'; cat tools/vault-links.py; } > "$prefix/bin/vault-links"
chmod 0755 "$prefix/bin/vault-links"
echo "bin/vault-links" >> "$manifest.tmp"
rm -rf "$share/plugin" "$share/graph" "$share/vault-scaffold"
cp -R plugin graph tools/vault-scaffold "$share/"
chmod +x "$share/plugin/hooks/agent-state" "$share/graph/serve.sh"
install -m 0644 tools/tickets-completion.zsh "$prefix/share/zsh/site-functions/_tickets"
echo "share/zsh/site-functions/_tickets" >> "$manifest.tmp"
mv "$manifest.tmp" "$manifest"

# --- config ---
mkdir -p "$config_dir"
cfg="$config_dir/config.json"
[[ -s "$cfg" ]] || echo '{}' > "$cfg"
runtime=""
for rt in podman docker; do
  if have "$rt" && "$rt" info >/dev/null 2>&1; then runtime="$rt"; break; fi
done
jq --arg p "$share/plugin" --arg g "$share/graph" --arg rt "$runtime" \
  '.pluginDir = $p | .graphDir = $g | (if $rt != "" and (.container // "") == "" then .container = $rt else . end)' \
  "$cfg" > "$cfg.tmp" && mv "$cfg.tmp" "$cfg"

echo "install.sh: installed into $prefix (commands in $prefix/bin)"
case ":$PATH:" in *":$prefix/bin:"*) ;; *) echo "  add $prefix/bin to your PATH" ;; esac
echo "  zsh completion: add $prefix/share/zsh/site-functions to fpath before compinit"
echo "  next: vault-init <name>, then see the README (Configuration)"
