#!/usr/bin/env bash
# Installs claude-tickets without Nix: ./install.sh [--prefix <dir>]
#   <prefix>/bin/ct                             the command (built with Go)
#   <prefix>/share/claude-tickets/              plugin/ and graph/, which ct
#                                               finds next to itself
#   <prefix>/share/{zsh,bash-completion,fish}   ct's shell completion
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
    -h|--help) sed -n '2,9s/^# \{0,1\}//p' "$0"; exit 0 ;;
    *) echo "install.sh: unknown argument: $1" >&2; exit 1 ;;
  esac
done
share="$prefix/share/claude-tickets"
manifest="$share/.installed"

if [[ "$mode" == uninstall ]]; then
  [[ -f "$manifest" ]] || { echo "install.sh: nothing installed under $prefix" >&2; exit 1; }
  while IFS= read -r f; do rm -f "$prefix/$f"; done < "$manifest"
  rm -rf "$share" "$prefix/libexec/claude-tickets"
  echo "install.sh: removed claude-tickets from $prefix (your vaults, workspaces and ~/.config/claude-tickets are untouched)"
  exit 0
fi

# --- dependencies ---
missing=()
have() { command -v "$1" >/dev/null 2>&1; }
have go || missing+=("go (1.26+, to build ct)")
for t in git tmux tar; do have "$t" || missing+=("$t"); done
have podman || have docker || missing+=("podman or docker (for the code graph)")
have claude || missing+=("claude (Claude Code)")
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "install.sh: missing: ${missing[*]}" >&2
  [[ "$mode" == check ]] && exit 1
  echo "install.sh: installing anyway; the commands that need them will fail until they're there" >&2
fi
have fzf || echo "install.sh: fzf (optional) isn't installed: vaults are picked from a numbered list" >&2
[[ "$mode" == check ]] && { echo "install.sh: all dependencies found"; exit 0; }

# --- files ---
have go || { echo "install.sh: go is needed to build ct" >&2; exit 1; }
mkdir -p "$prefix/bin" "$share" "$prefix/share/zsh/site-functions" \
  "$prefix/share/bash-completion/completions" "$prefix/share/fish/vendor_completions.d"
go build -o "$prefix/bin/ct" ./cmd/ct
# The bash scripts and settings of an earlier install
rm -rf "$prefix/libexec/claude-tickets" "$share/vault-scaffold"
rm -rf "$share/plugin" "$share/graph"
cp -R plugin graph "$share/"
chmod +x "$share/graph/serve.sh"
# The plugin's hooks run ct by its path: a session's PATH may not have it
sed -i.bak "s|\"ct hook |\"$prefix/bin/ct hook |g" "$share/plugin/hooks/hooks.json"
rm -f "$share/plugin/hooks/hooks.json.bak"
"$prefix/bin/ct" completion zsh > "$prefix/share/zsh/site-functions/_ct"
"$prefix/bin/ct" completion bash > "$prefix/share/bash-completion/completions/ct"
"$prefix/bin/ct" completion fish > "$prefix/share/fish/vendor_completions.d/ct.fish"
printf '%s\n' bin/ct share/zsh/site-functions/_ct share/bash-completion/completions/ct \
  share/fish/vendor_completions.d/ct.fish > "$manifest"

echo "install.sh: installed ct into $prefix/bin"
case ":$PATH:" in *":$prefix/bin:"*) ;; *) echo "  add $prefix/bin to your PATH" ;; esac
echo "  zsh completion: add $prefix/share/zsh/site-functions to fpath before compinit"
echo "  next: ct vault init <name>, then see the README (Configuration)"
