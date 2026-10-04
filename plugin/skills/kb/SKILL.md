---
name: kb
description: Answer questions about the system the Obsidian knowledge-base vault describes (repos, services, architecture, data flows, versions, past tickets) from the vault first, then the code graph and code; investigate unknowns (exploring branches and building in its own clones); file manual tickets for issues worth following up; and record what was learned as drafts in the vault's inbox/ for /tickets:save to promote. Use for /tickets:kb <question>, in kb sessions, and in any session started with claude-vault, whenever the user asks how something works, where something lives, why it was built that way, or what depends on what.
---

# /tickets:kb [question]

The vault is named in the system prompt (`claude-vault`) or in `CLAUDE.md`
(a `kb` or ticket session). If neither names one, the session has no
knowledge base: say so (relaunch with `claude-vault`, or use `kb`) and answer from the code graph and code only. Read the vault's
`AGENTS.md` once per session. Without a question, ask what the user wants
to know.

## Answer: vault first, then graph, then code

1. **The vault:**
   - **One repo:** its project notes `projects/<slug>/` (index, features,
     sequences, data, `<slug>-decisions.md`, recent logs).
   - **Across services:** `projects/system/` (service map, compatibility
     matrix, flows), plus `knowledge-base/` and `references/`.
   - **History:** `tickets/` (search by repo slug, feature or keyword, and
     read the summaries at the end of the ticket notes).

   Repos are named by slug, `<provider>-<owner>-<repo>`. A bare repo name
   matches a project note's `aliases`.
2. **The code graph** (`graphify` MCP server): find where something lives,
   who calls it, and the path between services (`query_graph`,
   `shortest_path`, `get_neighbors`). Node ids are prefixed with the repo's
   slug.
3. **The code**, read to confirm details. Main clones under `~/Projects` are
   read-only and on whatever branch they're on. In a `kb` session, look at
   another branch, tag or commit with `kb-repo checkout <repo> <ref>`: an
   exploration clone in the session that the code graph swaps in and rebuilds.
   `kb-repo fetch` gets the latest remote branches, `kb-repo graph <repo>`
   forces a rebuild, and `kb-repo ls` shows what's checked out. The clones
   are yours to experiment in: build, run tests, add debug output, or try a
   change to confirm a theory. **Never commit or push** (both are denied).
   The edits stay in the session, and `kb-repo checkout --force` discards
   them. A fix worth keeping becomes a ticket (below), not a commit.
4. **Atlassian, read-only and free tools only:** Jira issues, Bitbucket PRs,
   commits and repos, when history or ownership matters. Never use unified
   `search` or the Teamwork Graph tools, which cost Rovo credits.

Answer concisely. Cite the vault notes you used as wikilinks, and the code
as `path:line` with the repo slug. Say what the answer rests on: notes (may
be stale), the graph, or the code itself (current on the base branch).
Where they disagree, trust the code, and say which note is out of date.

## Unknowns: triage them

When neither the vault nor a quick look settles it:
- **Investigate:** trace it through the graph and code, check the repo's
  history (`git log` in its main clone), and the related tickets and PRs.
- **Say what couldn't be determined,** and what would settle it: who to
  ask, what to run, which environment to check. Never guess.
- If it turns into work (a bug, a missing feature, a risk, something that
  needs a deeper investigation), create a ticket for it (next section).

## Issues become tickets

When you find something that should be investigated or fixed, file it as a
**manual ticket**, so it enters the ticket workflow (`ticket-start <ID>`):
1. **Check first** that it isn't already tracked: search `tickets/` by repo
   slug and keywords, and Jira for synced tickets. If it is, add what you
   found to your answer and an `inbox/` draft instead.
2. Run `ticket-new --type <bug|investigation|dev|chore> "<summary>"`. It
   files into this session's vault and prints the new note,
   `tickets/MAN-<n>/MAN-<n>.md`.
3. **Fill it in:**
   - `## Description`: what you found and why it matters, with the
     evidence (`<slug>:path:line`, graph paths, commits, how to reproduce).
   - `## Acceptance criteria`: what "resolved" means.
   - `## Context / links`: related notes and tickets as wikilinks.
   - Frontmatter: `projects: [<slug>, ...]`, `priority` if clear, the
     dependency fields if it blocks or relates to other tickets, and the
     tag `from-kb`.
   - A review task for the user under `## Follow-ups`, so it shows up in
     `follow-ups.md`:
     `- [ ] Review this ticket filed by kb: check the findings, scope, type
     and priority, then ticket-start it, ignore it, or close it #follow-up
     ➕ <today>` (date from `date +%F`).

   Write only that note. The ticket is the user's from then on.
4. **Tell the user** what you filed. In an interactive session, if it's a
   judgment call (minor, maybe intended), ask before filing.

## Record what you learned: drafts in `inbox/`

Don't edit `projects/`, `knowledge-base/` or `references/` directly. Other
sessions may be writing there, and only `/tickets:save` writes them, under the vault
lock. Instead, for each durable fact that the vault lacked or had wrong:
- Write a draft note in `<vault>/inbox/`, using the matching template
  (`feature.md`, `sequence.md` with a mermaid diagram for a flow,
  `decision.md`, `knowledge.md`, `reference.md`, `project.md` for a repo
  with no notes yet).
- **Filename:** final, vault-unique and kebab-case, so `/tickets:save` can move it
  as-is.
- **Frontmatter:**
  - `target:` where it belongs (`projects/<slug>/sequences`,
    `projects/system/architecture`, `knowledge-base/<topic>`,
    `references`, ...)
  - `update-of: <note>` when it corrects or extends an existing note
  - `session: <yyyy-MM-dd>-<topic>`, so `/tickets:save` knows which drafts are this
    session's
  - the usual frontmatter from `AGENTS.md`
- Link the drafts as usual: at least 2 wikilinks, by bare note name.
- Tell the user what you drafted, and remind them that `/tickets:save` promotes it.

Keep the drafts to facts worth keeping: how things work, why they're built
that way, which versions and services are involved, and gotchas. Leave out
the conversation itself.
