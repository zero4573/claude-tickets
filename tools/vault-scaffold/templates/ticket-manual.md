---
title: {{title}}
summary:
created: {{date}}
updated: {{date}}
status: new
type: ticket
ticket-type: dev
priority:
source: manual
source-id: {{title}}
ignore: false
ignore-until:
ignore-reason:
parent:
children: []
covers: []
covered-by:
blocked-by: []
blocks: []
related: []
blocked: false
projects: []
versions: []
tags: [ticket, manual]
---
# {{title}}

<!-- A manual ticket: you write and manage it here, ticket-sync never
touches it. Name the note after its ID (MAN-<n>, the next free number;
`ticket-new "<summary>"` does it for you), set `summary` and `ticket-type`
(dev | bug | investigation | chore | epic), fill in the three sections
below, then run `ticket-start <ID>`. Agents read these sections but never
edit them. Set `status: closed` yourself when it's finished.
Dependencies: list tickets as "[[ID]]" in parent, blocked-by, blocks and
related, and set blocked: true while a blocker is open. To work several
tickets as one, make this a lead: ticket-type: epic, the tickets in covers,
and covered-by: "[[<this ID>]]" on each of them. -->

## Description
What needs doing and why. For a bug: what happens, what should happen, how
to reproduce it.

## Acceptance criteria
- [ ] 

## Context / links
Related tickets ([[TICKET-ID]]), projects ([[provider-owner-repo]]), docs,
target versions if known.

## Workspace
<!-- Filled in at kickoff by work-ticket: one row per repo (by slug) -->
| Repo (slug) | Base | Target version | Branch | Path |
|---|---|---|---|---|

## Tasks
<!-- Kept current by the ticket's agents (Tasks plugin format, #role/<role> tags) -->

## Questions
<!-- Questions the agents asked, with your answers -->

## Follow-ups
<!-- People to chase about tickets this one depends on (#follow-up tasks) -->

## Review
<!-- Written when the ticket reaches status: review -->

## Knowledge
<!-- Notes created or updated by /tickets:save -->
