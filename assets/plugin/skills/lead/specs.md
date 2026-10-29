# lead: specs

Read by the lead when the ticket's `board.md` has a `## Specs` checklist (the ticket changes
capability specs: `specs/<capability>/spec.md`, requirements `REQ-<PREFIX>-<n>`). Without one, plan as
`SKILL.md` says and ignore this file.

## Reading the change
- `ct spec diff <ID> [--stat]` is the ticket's spec change for every repo, wherever its specs live
  (repo or vault); never look for the location yourself. Only the PO writes specs: you, the
  tasks and the reviewers never edit `specs/`.
- `board.md ## Specs`, one line per capability: `- [ ] <slug>/<capability> · draft|ready|building|merged · needs <slug>/<capability>, …|- · <REQ IDs>`.
  The PO marks a capability `ready` and tells you; you set `building` when its tasks are planned
  and `merged` when they and their review are merged.

## Scheduling
- Plan only `ready` capabilities; a `draft` one waits for the PO.
- Tasks of different capabilities share a wave when their `owns` don't overlap. A capability that
  consumes another's `## Interfaces` waits until that one is `merged`.
- Each task file names the requirement IDs it implements in `requirements:` (the field replaces
  `contracts:` for anything a spec covers) and `## Do` says each ID's test cites it.
- `contracts/` holds only shapes private to this ticket's tasks (an internal Go signature);
  anything another capability or repo uses lives in the capability's `## Interfaces` or
  requirement text, never in a contract file.

## Review
- One review per capability once its tasks merge: the reviewer's prompt adds the capability, its
  requirement IDs and `ct spec diff <ID>`.
- The reviewer lists the IDs that pass (a `proposed` requirement implemented and cited by a test, no
  contradiction of an `implemented` one). Relay that list to the PO, who flips them to
  `implemented`; you never do. A failing ID is a fix task in the next wave.
- Hand-over (`handover.md`) waits until every `## Specs` line is `merged` and the PO has flipped the
  passed IDs.
