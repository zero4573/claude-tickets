# lead: reference

Detail the lead reads at one moment, moved out of `SKILL.md`. `SKILL.md` points
here by heading.

## Board line
Set a task's board line, in every state, with one recipe: it drops an old `✅`/`❌` date, sets box, word and
new date, and fails loudly when the line didn't change. `<c>`/`<status>`/`<date>`: `' '`/`planned`/none ·
`\/`/`running`, `orphaned`, `partial`, `blocked`, `pending — user`/none · `x`/`merged <sha>`/` ✅ $(date +%F)` ·
`-`/`dropped`/` ❌ $(date +%F)`. After a merge it is the last link of the merge chain (`... && sed ...`, with `merged $(git rev-parse --short HEAD)`):
```sh
sed -i -E "/^- \[.\] NN · /{s/ (✅|❌) [0-9-]{10}$//; s/^- \[.\] (NN) · [^·]* · (.*)$/- [<c>] \1 · <status> · \2<date>/}" board.md
rg -q "^- \[<c>\] NN · <status> · " board.md || echo "NN not updated"
```
Status words never contain `·` (nor does a note ` · `); the `[/]` box is written `\/` in `<c>` because `/` is the sed delimiter.

## Disposition line formats
The reviewer writes `- [ ] <ID> · open`; you replace it with one line per state (`✅`/`❌` dates from `date +%F`):
- `- [/] <ID> · fixing · task NN` while a fix task runs;
- `- [x] <ID> · resolved · task NN (<merge sha>) ✅ <date>` (or a commit sha) once a fix is merged;
- `- [-] <ID> · kept: <reason ≤150 chars> (<sha if a comment or doc was added>) ❌ <date>` for
  dropped, intended or not-applicable items (the reason is mandatory);
- `- [x] <ID> · deferred → [[<ticket>]] · <why> #deferred ✅ <date>` when it goes to a follow-up ticket.

## Gate script
```sh
for f in tickets/<source>/<ID>/reviews/*.md; do
  [ -e "$f" ] || continue
  rg -q '^## Dispositions' "$f" || echo "$f: no Dispositions"
  for id in $(rg -o '^- (W[0-9]+-[a-z0-9-]+-[MSC][0-9]+) · ' -r '$1' "$f" | sort -u); do
    rg -q "^- \[x\] $id · (resolved · |deferred → \[\[.*#deferred)|^- \[-\] $id · kept: " "$f" || echo "$f: $id"
    rg -q "^- \[.\] $id · (open|fixing)" "$f" && echo "$f: $id open line"
  done
done
```
Each printed `<file>: <ID>` (or `open line`: a leftover `open`/`fixing` line) needs a fix task in the next wave, a `kept:` reason or a deferral.

## Ticket types (they shape the plan, not the agents)
- `dev`: the plan of `SKILL.md` as written. `chore`: one task, or you do it directly; review only if behaviour can change.
- `bug`: follow the `tickets:diagnose` skill ("In the ticket workflow"). Wave 1's first task
  is "build the feedback loop" with `skills: [tickets:diagnose]` (phases 1–2, and 3 when
  cheap); from its report write `plan.md ## Root cause` (the loop command, the confirmed cause,
  the hypotheses ruled out; trace with graphify and the code). The fix task also lists
  `skills: [tickets:diagnose]`, runs the loop in `## Checks`, and its "Done when" names a
  regression test that fails before the fix.
- `investigation`: a vault-only task, or you write `investigation.md` (template
  `investigation`, ≤150 lines); code only if the ticket asks for it.
- `epic`: one plan over `covers`, scope and acceptance criteria per covered ticket; tasks carry
  `for: "[[ID]]"`; keep each covered ticket's `status` in step with its part of the work.

## Board archive
`board-archive.md` (`type: board-archive`; `## Tasks`, `## Reviews`) takes the lines moved from `board.md`
unchanged, checkboxes included. `<W>`: the older of the last two reviewed waves; only finished lines move:
```sh
t=$(mktemp); r=$(mktemp)
awk -F ' · ' -v W=<W> '/^- \[[x-]\] / && $2 ~ /^(merged|dropped)/ { n=$5; sub(/^w/, "", n); if (n+0 < W) print }' board.md > "$t"
awk -F ' · ' -v W=<W> '/^- w[0-9]+ / { n=$1; sub(/^- w/, "", n); if (n+0 < W) print }' board.md > "$r"
```
Append `"$t"`/`"$r"` under the archive's headings, drop them from `board.md` (`grep -vxFf`), check `wc -l`.

## Fix loops
At triage, check every finding's origin tag (`new`, `pre-existing`, `regression of <ID>`) against the
previous fix range: `git blame` its lines, then read the previous review's findings and the
`plan.md ## Decisions` entries that touch the same code.

Stop patching in an area when either holds:
- the finding is a regression of the last fix round (it breaks behaviour that round or an earlier
  finding secured);
- two fixes keep undoing each other: the same area was fixed in consecutive rounds, or the fix
  would reverse an earlier one.

Then write no fix task for that finding. Record the triage in `plan.md ## Decisions` (the finding,
the earlier fixes it loops with) and ask the user through the gate in one `Ask user:` with a design
option, the recommended design first. The review's other findings are fixed as usual.

## Crash-window contract
For a step that writes several files or trees, the contract (`contracts/<name>-v<N>.md`) has a table, one row per write:
`step | what a crash right after it leaves | how a rerun finishes or undoes it`. Every row needs a rerun path that
converges without manual repair; a missing one is a must-fix before any build task starts.

