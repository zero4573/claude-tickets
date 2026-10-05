---
name: recall
description: Recall where a project or ticket stands from the Obsidian vault (latest session logs, decision index, ticket task list) and summarize what's done and what's left. Use when the user runs /tickets:recall [project|ticket], or asks to pick up where things left off. (Named recall so it doesn't shadow Claude Code's built-in /resume, which reopens conversations.)
---

# /tickets:recall [project | ticket-key]

Read-only. Never write to the vault.

## Find the vault and the subject

- **Ticket workspace** (the cwd or a folder above it has a `workspace.json`
  whose `id` is the ticket ID; `~/Projects/work-<vault>/<ID>` by default): `CLAUDE.md` names
  the vault and the ticket.
- **A `kb` session** (`workspace.json` id `KB`, `<workRoot>/.kb-<vault>`):
  `CLAUDE.md` names the vault.
- **Otherwise:** the knowledge-base vault named in the system prompt
  (`ct claude`). Failing that, the vaults are the folders under
  `$OBSIDIAN_ROOT` (`~/Documents/Obsidian`) that contain `.obsidian/` (recall only reads);
  if there are several, ask which one with AskUserQuestion.
- **Subject:** the argument if given: a ticket ID like `PROJ-12` or
  `MAN-3`, or a project slug (`bitbucket-acme-billing-service`, a
  folder under `projects/`; a bare repo name is matched against the
  project notes' `aliases`, asking if several match). Otherwise the
  workspace's ticket, else the cwd repo's slug as the system prompt names
  it (`<provider>-<owner>-<repo>`, from its path under `~/Projects`). Also
  mention any drafts waiting in `inbox/` for that project, and open tasks
  in `inbox/` follow-up notes (`type: follow-ups`) that link its tickets.

## Project

1. The 3 most recent notes in `projects/<project>/logs/`, newest first by
   filename date.
2. `projects/<project>/architecture/<project>-decisions.md`, plus the decision notes
   it links that the logs mention.
3. The project's index note `projects/<project>/<project>.md`, if it
   exists, and open tickets that list the project in `projects:`
   (`tickets/*/*.md` with `status` other than `closed` or `done`).

## Ticket

1. The ticket note: its content (the source block, or a manual ticket's
   description sections), `## Workspace`, `## Tasks`, `## Questions`,
   `## Follow-ups`, `## Review`, and the summaries at the end. Also its
   dependency fields (`blocked-by`, `blocked`, `parent`).
2. The 3 most recent `tickets/<ID>/logs/` notes, the hand-off files' open
   questions and verdicts, and `pr-feedback.md` if there is one (threads
   still marked `needs you`, replies not posted yet).
3. **Leads and covered tickets:**
   - A lead (`ticket-type: epic`): each ticket in its `covers` list, briefly
     (status, its group in `## Tasks`), and the open `## Follow-ups` on
     children that aren't the user's.
   - A covered ticket (`covered-by: "[[<LEAD>]]"`): it's worked in the
     lead's workspace, so recall the lead too, and say that
     `ct start <LEAD>` is where to pick it up.
4. For each repo in `## Workspace`, steps 1 and 2 of **Project**.
5. In a workspace, `ct ws ls <ID>` for each worktree's branch and
   uncommitted changes.

## Summarize

Keep it short and concrete:
- **State:** where things stand (ticket status, last session's outcome).
- **Done:** recent work, with wikilinks.
- **Decisions in force:** the ones that constrain what comes next.
- **Open:** unanswered questions, unfinished tasks (`[ ]` / `[/]`),
  open follow-ups (who to chase), PR threads waiting on the user, pending
  items from the logs.
- **Next:** the next 1 to 3 concrete steps.
