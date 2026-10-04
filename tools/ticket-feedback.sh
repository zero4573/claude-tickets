# Shorthand for `ticket-start --feedback`: for each ticket, a sandboxed session
# running /tickets:pr-feedback <ID>, which applies the review feedback on your open
# Bitbucket PRs for the ticket (see the pr-feedback skill)
case "${1:-}" in
  -h|--help)
    echo "Usage: ticket-feedback [--force] [--no-fetch] <ID>...   (= ticket-start --feedback; Tab completes the IDs)"
    exit 0
    ;;
esac
exec ticket-start --feedback "$@"
