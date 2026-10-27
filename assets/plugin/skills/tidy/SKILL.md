---
name: tidy
description: Rewrite a ticket branch's unpushed commits into coherent ones with Jujutsu (/tickets:tidy), the tree unchanged byte for byte, in a scratch clone, then move the branch with a backup ref. Use when the user runs /tickets:tidy, asks to clean up or squash the ticket's history before saving and signing, or when a task agent has skills [tickets:tidy].
argument-hint: "[repo slug ...; default: every repo with unpushed commits]"
---

# /tickets:tidy [repo ...]

Order: docs → tidy → `/tickets:save` → `ct ws sign`. Run it after the docs wave: tidy
only rewrites history, never content.

## Started by the PO

The user runs `/tickets:tidy` in the PO session. Outside a ticket workspace (no `board.md`
next to `CLAUDE.md`), say so and stop. Otherwise append a `relay.md` entry
`R-nn · <date> · user→lead · request · tidy history: <repos or "all"> · open` and wake the
lead (SendMessage "new request R-nn"; no live lead: planned when the next one is spawned).

## The lead's part

1. One task per repo with unpushed commits on the ticket branch
   (`git rev-list HEAD --not --remotes`; none: skip the repo). The scratch clone dir is
   `<ws>/tasks/tidy-<slug>`. Give the task the immutable boundary, from the ticket worktree: the
   full ids on the `-` lines of `git rev-list --boundary HEAD --not --remotes`, plus the
   base tip when `git merge-base --is-ancestor <base tip> HEAD` holds.
2. After the task, in the ticket worktree: `git fetch <scratch clone> <bookmark>` (the new
   tip is `FETCH_HEAD`), then check all of these, else stop with nothing moved:
   - `git rev-parse FETCH_HEAD^{tree}` equals `<old>^{tree}`;
   - every boundary id: `git merge-base --is-ancestor <id> FETCH_HEAD`;
   - `origin/<branch>`, when it exists: `git merge-base --is-ancestor origin/<branch> FETCH_HEAD`.

   Then `git update-ref refs/tidy-backup/<branch> <old>` and move the branch with
   `git update-ref refs/heads/<branch> <new> <old>` (the old value guards a race). The
   tree is the same, so `git status --short` must stay empty: check.
3. Run the repo's checks, then `ct ws mark <ID> tidy`.
4. Record the old→new mapping (old range, new commit list) in the ticket note's
   `## Review`, and delete the scratch clones with `ct ws tidy-clean <ID>` (the sandbox denies `rm -rf`;
   it keeps an unmerged clone, `--force` only after the user agrees).

Never touch pushed commits, never `reset --hard`.

## The task agent's part

jj is Jujutsu (0.46 syntax; `jj help <cmd>` when unsure). Never colocate it in a ticket
worktree (it would write the main clone's `.git`): work only in the scratch clone.

1. `<old>` = the branch tip; the boundary ids come from the lead.
2. `jj git clone --colocate <ticket worktree> <scratch dir>` (the whole repo, so every
   boundary id exists in it; the clone already tracks the bookmark). Before any commit set,
   with `jj config set --repo`: `user.name`/`user.email` from the worktree's `git config`,
   and `ui.editor '["true"]'` (a list: a bare `true` is a TOML boolean jj rejects). Then set `revset-aliases."immutable_heads()"` to the boundary ids (a
   union by `|`, full ids) so jj refuses to rewrite anything not made for the ticket.
   Check `jj log -r '(<boundary ids>) & ~immutable()'` is empty (every boundary id
   immutable) and `jj log -r '<boundary ids>::'` shows only ticket commits as mutable.
3. Fold each fix-up commit into the commit it changes: `jj absorb --from <rev>` (a bare
   `jj absorb` reads the empty `@`), then `jj abandon <rev>` when it is left empty. Then
   `jj squash`, `jj split`, `jj describe` into one commit per coherent change; `jj rebase`
   to linearize (no merge commits); lines added then reverted disappear. Messages in the
   ticket's commit style with a why-body and no vault-only IDs (task numbers, waves, `R-nn`,
   `B-nn`, findings, a manual ticket's key). A rewrite gives descendants new commit ids:
   address commits by change id or relative revset (`@-`), never a stale commit id (it
   makes divergent copies). When the history is too tangled to fold, rebuild by concern
   instead: `jj new <start>`, `jj restore --from <old> <paths>`, describe, repeat.
   `jj op log` / `jj op restore` undo a mistake. Don't `git checkout` in the clone.
4. Verify the final tree equals `<old>^{tree}` byte for byte: `git rev-parse <new>^{tree}`
   in the clone (colocated, so git sees jj's commits). Different: `jj op restore`, fix,
   repeat.
5. Run the repo's checks on the final commit, move the bookmark to it:
   `jj bookmark set <ticket branch> -r <rev> --allow-backwards`. Report the commit list
   and the bookmark.
