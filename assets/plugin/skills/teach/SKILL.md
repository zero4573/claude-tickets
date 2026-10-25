---
name: teach
description: Teach the user a topic over many sessions (/tickets:teach <topic>, opened by ct teach), keeping the learning state in the vault's learning/<topic>/ folder (mission, resources, glossary, notes, short lessons, reference notes, learning records, HTML quizzes), grounded in primary sources found on the web.
disable-model-invocation: true
argument-hint: "<topic, kebab-case>"
---

# /tickets:teach <topic>

The vault is named in the system prompt (`ct teach`) or the session's `CLAUDE.md`; without
one, say so and stop. Read the vault's `AGENTS.md` once (`rg -n '^## '`, then "Note Rules").
No topic: list `ls <vault>/learning/` and ask (AskUserQuestion) which one, or a new one.
Before writing, `ct vault search "<subject>" --path learning/<topic>` for what's already taught.
The topic is kebab-case; it names the folder and prefixes every file, so names stay unique.

## Writer rule
This session owns **only** `learning/<topic>/` and writes it directly (no `/tickets:save`
promotion). One session per topic. Around every write that touches more than one file, hold
the lock: `ct vault lock acquire <vault> teach-<topic>` … `ct vault lock release <vault>
teach-<topic>`, releasing it even when something fails. Re-read a note before editing it.
The repos (read-only) and the code graph can supply examples; never edit them.

## Layout (`learning/<topic>/`)
| File | Template | Holds |
|---|---|---|
| `<topic>.md` | `learning-topic` | index: the mission, links to everything below |
| `<topic>-resources.md` | `base` | trusted sources: `## Knowledge`, `## Wisdom`, `## Gaps` |
| `<topic>-glossary.md` | `base` | the canonical terms |
| `<topic>-notes.md` | `base` | the user's preferences, your working notes |
| `lessons/<topic>-NNNN-<name>.md` | `lesson` | one lesson |
| `reference/<topic>-<name>.md` | `learning-reference` | cheat sheets, compressed essence |
| `records/<topic>-NNNN-<name>.md` | `learning-record` | what the user has shown they know |
| `quizzes/<topic>-NNNN-<name>.html` | none | the lesson NNNN's interactive quiz |

Everything but the quizzes is a Markdown note under the vault's rules: frontmatter (with
`topic: "[[<topic>]]"` and `projects: [<slug>, …]` for related repos), kebab-case names and
tags (`learning`, `<topic>`), at least 2 wikilinks by bare note name, the template named
above. `NNNN` counts up from `0001` per folder (`ls lessons/ | sort | tail -1`); a quiz shares
its lesson's number and name. Create subfolders when their first file is written. A lesson
links its quiz as a relative Markdown link, `[Quiz](../quizzes/<topic>-NNNN-<name>.html)`.
Each new lesson, reference note and record gets a line in `<topic>.md`'s matching list.

## Session start
Read `<topic>.md`, `<topic>-notes.md`, the glossary's term list, and the newest records
(`ls records/ | tail -5`). Then, the first that applies:
1. **No mission** (new topic, or `## Mission` empty): interview the user, in short rounds,
   on *why*: the concrete outcome, what success looks like (observable), constraints (time,
   budget, preferences), what's out of scope. Push back on vague answers ("learn X" is not a
   mission). Write it, ≤1 screen, into `<topic>.md`.
2. **Few resources** (under ~5 knowledge entries): research before teaching (below).
3. Otherwise teach the next lesson, or what the user asks for.
A mission changes only with the user's confirmation; record the change as a learning record.

## Resources (knowledge from primary sources)
Never teach from parametric knowledge. Find sources with **WebSearch**, read them with
**WebFetch**: official docs, specifications, standards, recognised experts' books and
articles, peer-reviewed work; no marketing posing as teaching. One annotated line each:
`- [<kind>: <title> by <author>](<url>): <what it covers>. Use for: <when>.` Communities go
under `## Wisdom` (high-reputation, moderated), missing areas under `## Gaps`. Prune sources
that proved wrong, shallow or off-mission. If the user opts out of communities, note it.

## Lessons
- Pick the lesson in the user's **zone of proximal development**: from the mission and the
  records, the most relevant thing that is just beyond what they've shown they know.
- **Short**, finishable in minutes, **one tangible win**, tied to the mission. Knowledge
  first (only what the win needs), then practice with immediate feedback.
- **Cite** every claim with a link to a resource, and end with `## Primary source`: the single
  best source to read or watch next. Remind the user to ask you follow-up questions.
- Open with 2–3 **retrieval** questions on earlier lessons (spacing: older ones too, not only
  the last); in skills practice, **interleave** related earlier skills.
- Use the glossary's terms exactly; link the lessons and reference notes it builds on.
- Write the reference note that compresses it (syntax, steps, a flowchart, a table): lessons
  are seldom reread, reference notes are.
- Show the lesson in the chat in short parts and work through the practice with the user;
  the files are the record they come back to.

## Quizzes (the only HTML)
One self-contained file per lesson: inline CSS and JS, no network, no external libraries,
readable in light and dark. 3–8 questions, mostly retrieval of this and earlier lessons.
Feedback right after each answer, with the reason and a link back to the source. All answers
to a question have **the same number of words** (and characters, as near as you can), the
correct one's position varies across questions, and nothing in formatting or order hints at
it. Tell the user the file's path to open.

## Learning records
Write one only when the user **shows** non-trivial understanding (an exercise done, a
question answered), states prior knowledge (with the depth claimed), corrects a misconception,
or the mission shifts. 1–3 sentences: what is now known and why it changes what to teach next;
`## Evidence` and `## Implications` only when they add something. Coverage is not learning,
and a record is not a session log. A record a later one contradicts gets `status: superseded`
and `superseded-by: "[[<newer>]]"`; never delete it.

## Glossary
Add a term only once the user uses it correctly. One or two sentences on what it *is*; pick
one word per concept and list the others as `_Avoid_:`; use the glossary's own terms inside
definitions; group under `##` headings when clusters appear; say how a loosely used term is
meant here; revise in place as understanding deepens. Every lesson keeps to it.

## Wisdom and preferences
Questions that need judgement from practice: give your best answer, then point to a
community from `## Wisdom` where the user can test it. Preferences the user states about how
they're taught ("shorter lessons", "more code") go into `<topic>-notes.md` at once and steer
every later lesson.

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.
