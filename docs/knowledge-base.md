# Knowledge base and vault search

The vault's `projects/` notes describe each repo (by slug), what it depends on, the
features and versions it shipped, and the decisions behind them. Agents look there before
reading code, and `/tix:save` writes new facts back.

```mermaid
sequenceDiagram
  participant A as agent or tix spec session
  participant S as tix vault search
  participant X as index (.tix/)
  participant N as tix vault note
  participant D as inbox/ draft
  participant V as /tix:save
  A->>S: search query, --project slug, --budget
  S->>X: embedding similarity, BM25 keywords, links
  X-->>S: ranked sections
  S-->>A: notes and sections within the budget
  A->>N: outline, then get note section
  N-->>A: one section, nothing else
  A->>A: Grep and Read the code: callers, paths between repos
  A->>D: durable fact found
  V->>D: promote to projects/, knowledge-base/, references/
```

## Search

`tix vault search` ranks sections by embedding similarity, BM25 and links. The index lives
in `<vault>/.tix/`, is rebuilt incrementally on search, and is never edited by hand.
The embedding model is fetched once on the host with `tix vault model fetch` into the
cache; a sandbox only reads it. Without the model, search is keyword-only and says so.
`tix vault note outline|get|set-section|append|frontmatter` reads or writes one section and
leaves every other byte as it was.

## Without a ticket

`tix spec "<question>"` opens a session with the vault and every main clone read-only. It
answers from the vault first, then the code.
`tix spec repo checkout <repo> <ref>` makes an exploration clone to build and test in; it is
never committed. A durable fact becomes a draft in `inbox/`; an issue worth following up
becomes a manual ticket with `tix new`. `tix claude` gives a plain repo session the same
skills.

## Relations between projects

A project lists what it needs in `depends-on`; projects sharing flows can join a group
note. `tix vault groups` prints the clusters and `--check` validates the links.
