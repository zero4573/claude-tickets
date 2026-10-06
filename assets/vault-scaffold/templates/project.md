---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: project
repo: <provider>/<owner>/<repo>
aliases: [<repo>]
groups: []
depends-on: []
tags: [project]
---
# {{title}}

The project note for one repo, named by its slug `<provider>-<owner>-<repo>`
(e.g. `github-acme-billing`); `aliases` keeps the short repo name
searchable. What this project does, in a paragraph.

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
- Features: `features/`
- Data flows: `sequences/` (flows shared with a group live in the group's `sequences/`)
- Data model: `data/`
- Session logs: `logs/`
