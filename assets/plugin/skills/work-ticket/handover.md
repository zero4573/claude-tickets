# work-ticket: hand-over

Read by the PO (`/tickets:work-ticket <ID>`) when the lead's final block ends with
`Next: handover-done`. It ends with `lead: none` and the user told the ticket is ready for
review, or with the lead woken again because a review finding is still open.

## 1. Review-disposition gate
First `cd <vault>` (your cwd is the workspace) and run the review-disposition gate for <ID>:
```sh
for f in tickets/<ID>/reviews/*.md; do
  [ -e "$f" ] || continue
  rg -q '^## Dispositions' "$f" || echo "$f: no Dispositions"
  for id in $(rg -o '^- (W[0-9]+-[a-z0-9-]+-[MSC][0-9]+) · ' -r '$1' "$f" | sort -u); do
    rg -q "^- \[x\] $id · (resolved · |deferred → \[\[.*#deferred)|^- \[-\] $id · kept: " "$f" || echo "$f: $id"
    rg -q "^- \[.\] $id · (open|fixing)" "$f" && echo "$f: $id open line"
  done
done
```
If it prints anything, wake the lead with it (fix tasks, `kept:` reasons or deferrals) instead
of accepting. Each printed `<file>: <ID>` needs a fix task, a `kept:` reason or a deferral; `open line` is a leftover `open` or `fixing` line.

## 2. Accept
If it prints nothing, set `lead: none`, tell the user the ticket is ready for review
(`ct ws diff <ID>`, changes can be asked for here; then, in the user's order, docs, tidy,
`/tickets:save`, `ct ws sign <ID>` on the host (`--skip-marks` if they chose to skip docs or tidy), push), and suggest a refresh (`SKILL.md` 6).
