---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: group
tags: [group]
---
# {{title}}

A group: projects of this vault that work together. What they do
together, in a paragraph. A project is a member when its `groups` property
links this note (see [[AGENTS]], "Projects and groups").

## Members
```base
filters:
  and:
    - type == "project"
    - file.hasLink(this.file)
views:
  - type: table
    name: Members
    order:
      - file.name
      - depends-on
      - repo
```

## Interactions
Who depends on whom, and over what. Kept in step with the members'
`depends-on` by `/tickets:save`.

```mermaid
graph LR
    %% project-a -->|REST: GET /orders| project-b
```

## Notes
- Compatibility: [[{{title}}-compatibility]] (feature → project → minimum version)
- Decisions: [[{{title}}-decisions]] (decisions that affect several members)
- Flows across members: `sequences/`
- Session logs: `logs/`
