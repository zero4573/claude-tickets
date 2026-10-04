---
name: pr-feedback
description: Review the feedback on the user's open Bitbucket pull requests for the current ticket workspace (~/work/<ID>) and apply it in the ticket's worktrees, committing it on the PR branches but never pushing, then record what was done and draft a reply per comment in the vault. Launched by ticket-feedback / ticket-start --feedback as /tickets:pr-feedback <ID>, or run in an open ticket session; use when asked to address, apply, or go through PR review comments.
---

# /tickets:pr-feedback [<ID>]

You handle the review feedback on the user's **own open PRs** for the
ticket of the current workspace. Your cwd is `~/work/<ID>`, and its
`CLAUDE.md` names the vault and the ticket. Without an argument, use that
ticket. Outside a ticket workspace, say this only works in one (start it
with `ticket-feedback <ID>`) and stop.

The ground rules of `work-ticket` apply:
- **Vault writes:** in the vault, write only inside `tickets/<ID>/`. A lead
  ticket's session may also write the work sections and `status` of the
  tickets it `covers` (see `work-ticket`).
- **Git:** commit the applied feedback on the PR's branch (one commit per
  thread or coherent group, subject starting with the ticket ID), unsigned.
  Never push, rewrite commits already on a remote (the PR's existing
  commits), reset, or remove worktrees. The user reviews, signs
  (`ticket-ws sign <ID>`), pushes, and posts the replies.
- **Bitbucket is read-only:** the token has no write scopes, and you never
  post, resolve, approve or edit anything there. Replies are drafted in the
  vault.
- **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard"): never
  `search` or the Teamwork Graph tools, not even through `executeRead`.
- **Code:** query the `graphify` graph before reading code.

## 1. Find the PRs

Bitbucket goes through the `atlassian` MCP server. For each operation, use
`discover`, then `executeRead`. Bitbucket calls need no `cloudId`.

**Auth errors:** a 401 or 403 from a Bitbucket call means the token lacks
the `read:bitbucket:agent-interface` scope, or the account can't read that
workspace (see the vault's `references/atlassian-rovo-mcp-setup.md`). Stop at
the first one and report it, with the call and the workspace. Don't retry
with other tools or workspaces.

1. Read `workspace.json`. Bitbucket repos have `provider: bitbucket`, and
   their `owner` is the Bitbucket workspace slug.
2. For each Bitbucket workspace among them, plus any whose repos the ticket
   names, run `listMyBitbucketPullRequests` (`workspaceId`, state `OPEN`).
   The listing has no branches, so take each PR's repo from
   `links.html.href` (`https://bitbucket.org/<workspace>/<repo>/pull-requests/<n>`)
   and get the branches with `getBitbucketRepoPullRequest`.
3. A PR belongs to this ticket when either:
   - its source branch is a branch in `workspace.json`, or
   - the ticket ID, or for a lead ticket the ID of any ticket in its
     `covers` list, appears in its source branch or title, matched
     case-insensitively (`feature/PROJ-64`, `Feature/PROJ-64-x`,
     `PROJ-64: ...`).
4. For a matching PR whose repo has **no worktree** yet, add one on the PR's
   own branch:
   `ticket-ws add <ID> bitbucket/<workspace>/<repo> --base <destination> --branch <source>`.
   If the repo isn't cloned under `~/Projects/bitbucket/<workspace>/`, give
   the user the host command, `ticket-ws clone bitbucket/<workspace>/<repo>`,
   and ask whether it's done. Once it is, add it as above (it becomes a
   shared clone). If the user skips it, list the PR as skipped.
5. **Sync the worktree with the PR.**
   - Compare `git -C <worktree> rev-parse HEAD` with the PR's
     `source.commit.hash` (Bitbucket gives a short hash, so match the
     prefix).
   - The sandbox has no git credentials, so it can't fetch: use the refs
     already there. `ticket-feedback` fetches every main clone before the
     session starts, and `ticket-ws fetch` (host only) passes the new refs
     on to shared clones. A shared clone (`.git` is a folder, not a file)
     can also take its main clone's refs itself, which works in the
     sandbox: `git -C <worktree> fetch --quiet --prune <main clone>
     '+refs/remotes/origin/*:refs/remotes/origin/*'`, the main clone being
     `~/Projects/<provider>/<owner>/<repo>`.
   - If `origin/<source>` has the commit and the worktree is clean and
     behind, run `git -C <worktree> merge --ff-only origin/<source>`.
   - If the commit isn't there at all, tell the user to run
     `ticket-ws fetch <slug>` on the host, or to restart with
     `ticket-feedback <ID>`, which fetches. Skip that PR.
   - If the worktree has uncommitted changes, ask before going further.

No matching PRs: say so and stop.

## 2. Collect the feedback

For each PR:
- **Comments:** `listBitbucketRepoPullRequestComments` with
  `view: "full"`, paging with `page` until done. The useful fields are `id`,
  `content.raw`, `user.account_id` / `display_name`, `inline.path` and
  `inline.to` (new line) or `inline.from` (old line), `parent.id` (a reply),
  `resolution` (resolved), `deleted`, `pending` (an unsubmitted draft) and
  `updated_on`.
- **Tasks:** `listBitbucketRepoPullRequestTasks`, keeping the unresolved
  ones.
- **Reviewer states:** the participants with `state: changes_requested`
  show who is waiting on you.

Group comments into threads (a top-level comment plus its replies). Skip:
- deleted, pending and resolved threads;
- threads where the last word is the PR author's (the user's
  `account_id` is the PR's `author.account_id`), unless a reviewer wrote
  after it;
- threads already recorded in `tickets/<ID>/pr-feedback.md` (below) whose
  newest `updated_on` hasn't changed since.

Everything else is **open feedback**.

## 3. Triage each thread

Read the code each thread points at (the file at `inline.path`, around the
line), the PR diff where needed (`getBitbucketRepoPullRequestDiff`), and the
ticket's hand-off files for why it was written that way. Then classify it:

| Kind | What to do |
|---|---|
| **change**: a clear request (rename, fix, add a test, handle a case) | apply it |
| **question**: "would this…?", "why…?" | find out the actual answer, from the code, the graph and by running or writing a test. If the answer shows a bug, it becomes a **change**; otherwise draft an explanation |
| **suggestion / nit**: optional | apply if it's small and clearly better; otherwise draft a reply saying why not |
| **disagree**: you think the reviewer is wrong, or the change has real tradeoffs | don't change code; ask the user (below) |
| **unclear** | ask the user |

When several threads ask the same thing (e.g. the same question on several
files), handle them together and draft a reply for each.

**Ask the user** with AskUserQuestion, batched, for every "disagree" or
"unclear" thread, and for any change that's large or touches contracts,
schemas or other services. Recommend an option. Record the questions and
answers in the ticket note's `## Questions`, as `work-ticket` does.

## 4. Apply

- **Track it:** add a line to the ticket note's `## Tasks`, `Address PR #<n>
  review feedback (<k> threads) #role/developer ➕ <today>`, with a subtask
  per change. Set the ticket's `status: in-progress`.
- **Lead tickets:** the PRs of covered tickets are handled here too. Add the
  task line to the covered ticket's `## Tasks` as well, ending with
  `(in [[<lead ID>]])`, and set the `status` of each covered ticket whose PR
  you work on as you set the lead's (`in-progress`, then `review`).
- **Who edits:** do small changes yourself. Hand larger ones to the
  `developer` subagent with the threads, the files, and the expected
  behaviour.
- **Check:** after the changes, run the repo's tests and linters, and add a
  regression test where a comment found a bug. Run the `qa` subagent if the
  changes touch behaviour other services rely on.
- Changes are committed on the PR's branch in the worktree, on top of its
  pushed commits, and not pushed.

## 5. Record and hand over

Write `tickets/<ID>/pr-feedback.md` (template `templates/pr-feedback.md`; on
later runs, update it). Use one section per PR (`## PR #<n>: <title>`, with
its link) and one table row per thread:

| Thread | Where | Reviewer | Ask | Outcome | Draft reply |
|---|---|---|---|---|---|
| 873374177 | `api/handler.go:671` | Jane Doe | typed-nil check | applied: … | "Good catch, …" |

- **Outcome** is one of:
  - `applied: <what changed>`
  - `answered: <short answer>` (no change needed)
  - `declined: <why>` (only after the user agreed)
  - `needs you: <what's open>`
- **Draft reply:** ready to paste into the Bitbucket thread. Short and
  specific: say what changed and where, or answer the question.
- **Thread** is the top comment's `id`. Store each thread's newest
  `updated_on` in the note's frontmatter (`threads: {<id>: <updated_on>}`),
  so the next run skips threads that haven't changed.

Then:
1. Mark the tasks done (in covered tickets too) and set the ticket's
   `status: review`.
2. Add to `## Review`: the PRs handled, what changed per repo
   (`ticket-ws diff <ID> --stat`), test results, and the threads that still
   need the user.
3. Tell the user, in a few lines:
   - how many threads were applied, answered, or need them
   - that `pr-feedback.md` has the replies to post
   - that the changes are committed, not pushed, on the PR branches: review
     them, run `ticket-ws sign <ID>` on the host, then push to update the
     PRs.
