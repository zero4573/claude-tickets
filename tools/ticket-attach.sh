usage() {
  cat <<'EOF2'
Usage: ticket-attach <ID>   switch to the ticket's window in the current vault's tmux session
       ticket-attach --list print the tickets with an open window: <ID> <status> <summary>,
                            tab-separated (what completion uses)
EOF2
}
key="" list=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --list) list=1; shift ;;
    -*) usage >&2; exit 1 ;;
    *) key="$1"; shift ;;
  esac
done
require_vault
if [[ "$list" == 1 ]]; then
  windows="$(tmux list-windows -t "=$tmux_session" -F '#W' 2>/dev/null || true)"
  [[ -n "$windows" ]] || exit 0
  list_tickets "$vault" --all | awk -F '\t' 'NR == FNR { open[$0] = 1; next } $1 in open' <(echo "$windows") -
  exit 0
fi
[[ -n "$key" ]] || { usage >&2; exit 1; }
tmux list-windows -t "=$tmux_session" -F '#W' 2>/dev/null | grep -qxF "$key" \
  || die "no window for $key in tmux session '$tmux_session' (start it with ticket-start $key)"
tmux select-window -t "=$tmux_session:$key"
if [[ -n "${TMUX:-}" ]]; then
  tmux switch-client -t "=$tmux_session"
else
  exec tmux attach-session -t "=$tmux_session"
fi
