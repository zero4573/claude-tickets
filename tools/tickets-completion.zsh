#compdef ticket-start ticket-feedback ticket-attach ticket-open
# zsh completion for the ticket commands (installed as _tickets by
# tickets.nix). Ticket IDs come from the current vault (vault-default):
# `ticket-start --list` (its open tickets) for ticket-start and
# ticket-feedback, `ticket-attach --list` (tickets with an open window) for
# ticket-attach, `ticket-open --list` (tickets with a workspace) for
# ticket-open. Each is shown with its summary and status; IDs already on
# the command line aren't offered again.

_tickets_ids() {
  local lister=ticket-start id st summary
  local -a ids
  [[ $service == ticket-attach || $service == ticket-open ]] && lister=$service
  while IFS=$'\t' read -r id st summary; do
    [[ -n $id ]] || continue
    (( ${words[(Ie)$id]} && ${words[(Ie)$id]} != CURRENT )) && continue
    ids+=("$id:${summary:-(no summary)} [$st]")
  done < <(command $lister --list 2>/dev/null)
  _describe -t tickets 'ticket' ids
}

case $service in
  ticket-attach)
    _arguments \
      '(- *)'{-h,--help}'[show help]' \
      '--list[print the tickets with an open window]' \
      '1:ticket:_tickets_ids'
    ;;
  ticket-open)
    _arguments \
      '(- *)'{-h,--help}'[show help]' \
      '--list[print the tickets that have a workspace]' \
      '1:ticket:_tickets_ids'
    ;;
  ticket-feedback)
    _arguments \
      '(- *)'{-h,--help}'[show help]' \
      '--no-attach[with one ticket, don'"'"'t switch to its window]' \
      '--force[also tickets marked ignore, or covered by an open lead]' \
      '--no-fetch[skip fetching the main clones first]' \
      '--no-attach[with one ticket, don'"'"'t switch to its window]' \
      '*:ticket:_tickets_ids'
    ;;
  *)
    _arguments \
      '(- *)'{-h,--help}'[show help]' \
      '--force[also tickets marked ignore, or covered by an open lead]' \
      '--no-fetch[skip fetching the main clones first]' \
      '--feedback[run /tickets:pr-feedback instead of /tickets:work-ticket]' \
      '(*)--list[print the open tickets]' \
      '--all[with --list: done and closed tickets too]' \
      '*:ticket:_tickets_ids'
    ;;
esac
