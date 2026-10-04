---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: project
repo: <provider>/<owner>/<repo>
aliases: [<repo>]
tags: [project]
---
# {{title}}

The project note for one repo. It's named by the repo's slug,
`<provider>-<owner>-<repo>` (e.g. `bitbucket-acme-billing-service`), and
`aliases` keeps the short repo name searchable. Describe what this service
does in a paragraph. Part of [[system]], see [[service-map]].

## Talks to
- [[provider-owner-other-service]]: what over what (REST, queue, DB)

## Notes
- Decisions: [[{{title}}-decisions]] (`architecture/{{title}}-decisions.md`)
- Features: `features/`
- Data flows: `sequences/` (cross-service flows live in `projects/system/sequences/`)
- Data model: `data/`
- Session logs: `logs/`
