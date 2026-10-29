---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: project
repo: <provider>/<owner>/<repo>
aliases: [<repo>]
specs:
groups: []
depends-on: []
tags: [project]
---
# {{title}}

The project note for one repo, named by its slug `<provider>-<owner>-<repo>`
(e.g. `github-acme-billing`); `aliases` keeps the short repo name
searchable. What this project does, in a paragraph.

`specs` says where the repo's specs live: `repo`, `vault`, or empty (not
asked yet); set it with `ct spec location <slug> [repo|vault]`.

A project stands alone unless it lists the projects it needs in
`depends-on` and the groups it belongs to in `groups` (see [[AGENTS]],
"Projects and groups"). Projects that depend on this one show in its
backlinks.

## Depends on
Projects in this vault this one needs (also in `depends-on`), and outside
dependencies worth knowing.

| Project | How (interface, contract) | Since | Flow |
|---|---|---|---|

## Notes
- Decisions: [[{{title}}-decisions]] (`architecture/{{title}}-decisions.md`)
- Specs: `specs/` (only when `specs: vault`; with `repo` they live in the repo's `specs/`)
- Features (index of tickets and versions): `features/`
- Session logs: `logs/`
