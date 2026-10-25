---
name: wave-review
description: Diff-scoped, capped wave review of one repo for a ticket (architect and senior developer in one pass), written to tickets/<ID>/reviews/wave-N-<slug>.md. Use when you are spawned as tickets:reviewer or tickets:reviewer-deep after a wave's tasks are merged.
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
4. Read by hunk (`git diff <range> -- <file>`); open full files only where a hunk needs
   context. Find callers with the fff tools (`mcp__fff__grep`, `mcp__fff__multi_grep`) when
   available, else `rg`, scoped to `<worktree>` (fff indexes the whole workspace, `.tasks/`
   included). Read the repo's `AGENTS.md` by section (`rg -n '^## '`), not whole.

## What to look for (both hats)
- **Architect:** boundaries and dependency direction, the ticket's contracts
  (`tickets/<ID>/contracts/`) kept exactly, the repo's conventions in its `AGENTS.md`, and
  whether the next planned tasks (the board's `planned` rows) fit without rework.
- **Senior:** correctness, error handling, races and resource leaks, input limits, injection
  and authz, cross-platform and compatibility issues, test gaps on the changed behaviour.
Run only cheap checks (the repo's vet or lint). Tests already ran at merge.

## Output (hard caps)
Write `<vault>/tickets/<ID>/reviews/wave-<N>-<slug>.md`: the vault's frontmatter plus
`type: review`, `wave`, `repo` (slug), `range`, `model`; link `[[<ID>]]` and the wave's task
files. Then:
- summary ≤5 lines;
- at most **3 must-fix, 6 should-fix, 5 consider**, each ≤6 lines:
  `W<N>-<repo>-<M|S|C><nn> · file:line · problem · fix · test to add`
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
