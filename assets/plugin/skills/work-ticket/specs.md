# Spec diff and ready capabilities (PO)

Only the PO writes specs (never the lead, tasks or reviewers). Specs live per repo as the project
note's `specs:` says; `tix spec location <slug>` prints where.

- **Repo location:** edit `specs/` in the repo's checkout on the ticket branch and commit it there.
- **Vault location:** never edit `projects/<slug>/specs/` directly. Edit a copy in the ticket folder,
  `tickets/<source>/<ID>/specs/<slug>/` (same layout), made only with
  `tix spec start <ID> <slug> [<capability>…]` (it copies the capabilities and snapshots
  `projects/<slug>/specs/` the first time; run it again to add capabilities; never copy by hand).
  `/tix:save` promotes it to `projects/<slug>/specs/`. Nothing spec-related goes into the repo.
- **Read the change** with `tix spec diff <ID>` (`--stat` for a summary): every repo of the
  workspace, wherever its specs live. The lead, reviewers and save read it the same way.
- **Kickoff:** read `spec-draft.md` and the affected specs first, then write the diff.
- **`## Specs` on `board.md`:** one line per capability,
  `- [ ] <slug>/<capability> · draft|ready|building|merged · needs <slug>/<capability>, …|- · <REQ IDs>`.
  Add a line when the diff adds a capability. Mark it `ready` when it has no open clarification
  markers and every capability it needs is ready, then SendMessage the lead "ready: <slug>/<capability>"
  at once and keep clarifying the rest. The lead marks `building` and `merged`.
- **After a review** that lists the requirement IDs it found implemented and tested, flip those from
  `proposed` to `implemented` in the spec yourself (reviewers and the lead don't write specs).
