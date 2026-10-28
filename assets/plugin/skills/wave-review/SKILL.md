---
name: wave-review
description: Diff-scoped, capped wave review of one repo for a ticket (architect and senior developer in one pass), written to tickets/<source>/<ID>/reviews/wave-N-<slug>.md. Use when you are spawned as tickets:reviewer or tickets:reviewer-deep after a wave's tasks are merged.
---

# Wave review (one repo, one wave)

Read-only: no code edits, no commits, no branch changes. The lead's prompt (the PO session
spawns you with it) passes: the ticket, wave
`N`, the repo slug and its ticket worktree, the range `<last-reviewed>..<head>`, the previous
fix-task IDs, the previous review file (if any), and a ≤5-line graph impact note.

## Scope it before reading anything
1. `git -C <worktree> log --oneline --merges <range>`: the wave's merges.
2. `git -C <worktree> diff --stat <range>`: only these files are in scope.
3. Vault decisions the diff touches: `ct vault search "<topic>" --project <slug> --budget 1500`.
   Previous findings: read the previous review's `## Dispositions`; any ID whose line says `open` or `fixing`
   is still open (`rg -n '^- \[.\] W[0-9]+-.* · (open|fixing)' <prev review>`). Check
   those against the fix tasks the lead named. Don't re-review unchanged code.
3b. Vault-only IDs (contract `commit-rules-v1`): `ct ws diff-check -C <worktree> --ticket <ID> <range>`
   flags the ticket's own IDs (its key, the relay's `R-nn`, the backlog's `B-nn`, finding IDs) in added
   lines and commit messages; a `vault-id` line is a must-fix, except a hit on a line kept from the base or in a fixture
   that models a ticket: note it, don't must-fix it. Keep a pattern scan for task and wave
   wording only: `git -C <worktree> diff <range> | rg '^\+' | rg 'task [0-9]+|wave [0-9]|\bw[0-9]\b'`
   (fixtures modelling tickets excepted). A hit is a must-fix.
4. Read by hunk (`git diff <range> -- <file>`); open full files only where a hunk needs
   context. Find callers with the fff tools (`mcp__fff__grep`, `mcp__fff__multi_grep`) when
   available, else `rg`, scoped to `<worktree>` (fff indexes the whole workspace, `tasks/`
   included). Read the repo's `AGENTS.md` by section (`rg -n '^## '`), not whole.

## What to look for (both hats)
- **Architect:** boundaries and dependency direction, the ticket's contracts
  (`tickets/<source>/<ID>/contracts/`) kept exactly, the repo's conventions in its `AGENTS.md` and the
  ticket's `tickets/<source>/<ID>/conventions.md` if it exists (by section), and
  whether the next planned tasks (the board's `planned` rows) fit without rework.
- **Senior:** correctness, error handling, races and resource leaks, input limits, injection
  and authz, cross-platform and compatibility issues, test gaps on the changed behaviour.
**Host-trust checklist (reviewer-deep only; ≤5 lines of findings):** for code that reads, copies or runs worktree files outside the session sandbox, check:
- symlinks, and symlinked or linked parent directories, followed or copied;
- special files (FIFOs, devices, sockets) opened or read;
- path escapes (`..`, absolute paths, case or Unicode aliases) out of the intended root;
- TOCTOU between a check and the use of the same path;
- running built code (scripts, hooks, build outputs) from an untrusted tree.
Run cheap checks (the repo's vet or lint), export the DB env from `conventions.md ## Test DB` if it has one, then run the **focused tests of the changed packages, DB suites included,** within a
**5-minute cap**: `timeout 300 <the repo's test command scoped to those packages>` (e.g. `timeout 300 go test -p 4
./pkg/a/...`); a timeout or a failure you can't reproduce is a finding, never a hang; name an overrun. When the ticket has a `## Test DB` or a wave task is `db: heavy`, any skipped DB test is a must-fix (M) finding; without either, skipped DB tests are no finding. Mark each finding
`confirmed` (a test fails or you reproduced it) or `unconfirmed` (reading only).

## Output (hard caps)
Write `<vault>/tickets/<source>/<ID>/reviews/wave-<N>-<slug>.md`: the vault's frontmatter plus
`type: review`, `wave`, `repo` (slug), `range`, `model`; link `[[<ID>]]` and the wave's task
files. Then:
- summary ≤5 lines;
- at most **3 must-fix, 6 should-fix, 5 consider**, each ≤6 lines:
  `W<N>-<repo>-<M|S|C><nn> · file:line · problem · fix · test to add · confirmed|unconfirmed`
  (`<repo>` is the slug's repo part, e.g. `W2-billing-service-M01`);
- "keep as pattern" ≤5 bullets;
- previous findings: one line listing resolved and still-open IDs;
- `## Dispositions` (last section): one line `- [ ] <ID> · open` for every finding ID you wrote
  in must-fix, should-fix and consider (the ID exactly as in the finding). The lead replaces
  the box and word (`[x] resolved`, `[-] kept`, `[x] deferred`, `[/] fixing`); you never do, and you never file tickets.
Must-fix means data loss, security, a broken contract, or a blocker for the next wave.
Everything else is should-fix or consider. Findings are plain bullets; only the Dispositions lines are checklist lines.

Reply in ≤15 lines: the counts, the must-fix one-liners, the file path. The reply goes to
your spawner, the PO, which tells the lead; the lead reads the review file.
