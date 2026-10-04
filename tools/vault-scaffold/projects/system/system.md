---
title: system
created: 2026-10-02
updated: 2026-10-02
status: active
type: project
tags: [project, system, microservices]
---
# system

The pseudo-project for everything that spans services: which services talk
to which, flows across services, and which versions of each service work
together. Each service's own notes live in `projects/<slug>/` (slug = `<provider>-<owner>-<repo>`) and link here.

- [[service-map]]: services and how they talk (mermaid)
- [[compatibility-matrix]]: feature → service → minimum version
- `sequences/`: data flows across services, one note per flow
- [[system-decisions]]: decisions that affect several services

Written by `/tickets:save` at the end of ticket sessions (see [[AGENTS]]).
