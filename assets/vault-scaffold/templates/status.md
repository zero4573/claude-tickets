---
title: TICKET-ID status
created: {{date}}
updated: {{date}}
status: active
type: ticket-status
ticket: "[[TICKET-ID]]"
tags: [ticket-status]
---
# TICKET-ID status

Live task views of [[TICKET-ID]] (Tasks plugin) over this folder: the ticket note's `## Tasks`
and `## Follow-ups`, the board's tasks, review dispositions and backlog items.

## Pending
```tasks
not done
filter by function task.file.path.startsWith(query.file.folder)
group by filename
```

## Done
```tasks
status.type is DONE
tags do not include #deferred
filter by function task.file.path.startsWith(query.file.folder)
group by filename
sort by done reverse
```

## Deferred
```tasks
tags include #deferred
filter by function task.file.path.startsWith(query.file.folder)
group by filename
```
