---
name: diagnose
description: Disciplined diagnosis of a hard bug or performance regression in five phases (build a tight pass/fail feedback loop, reproduce and minimise, test ranked hypotheses, fix, regression test). Used by the ticket lead for a bug's root cause and by task agents whose task file lists skills [tix:diagnose]; use whenever something is broken, throwing, failing or slow and the cause isn't known.
---

# diagnose

Don't skip a phase without saying why. Read the repo's `AGENTS.md` and any glossary or
decision notes for the area first.

**Redact.** Commands, output and captured artifacts you show carry `<REDACTED>` in place of
every secret. Keep credentials in env vars the loop reads, and quote only the lines that carry
the signal. If redacted output isn't enough, say so and ask (in a task: report `blocked`).

## 1. Build a feedback loop (most of the effort goes here)
A **tight** pass/fail signal that goes red on *this* bug finds the cause; without one, reading
code won't. Try, roughly in this order:
- a failing test at the seam that reaches the bug (unit, integration, e2e);
- an HTTP script against a running dev server, or a CLI run on a fixture diffed against a
  known-good snapshot (testscript `.txtar` files are good for this);
- a headless browser script asserting on DOM, console or network;
- a captured request, payload or event log replayed through the code path in isolation;
- a throwaway harness: one component with mocked dependencies, one function call;
- a property or fuzz loop for "sometimes wrong" (1000 random inputs);
- a bisect harness for "it worked at X": `git bisect run <script>`;
- a differential run: old vs new version (or two configs) on the same input, diffed;
- last resort, a scripted human step: the script prompts, the human acts, output comes back.

**Tighten it:** faster (cache setup, narrow scope), sharper (assert the exact symptom, not
"didn't crash"), deterministic (pin time, seed RNG, isolate files and network).
**Flaky bugs:** raise the reproduction rate (loop 100×, parallelise, stress, narrow timing)
until it's debuggable; 50 % is workable, 1 % isn't.
**No loop possible:** stop and say so with what you tried, and ask for access to an
environment that reproduces it, a redacted artifact (log, HAR, dump), or permission for
temporary instrumentation. Don't hypothesise without a loop.

**Done when** you can name **one command** you have already run (show it and its redacted
output) that drives the real code path, asserts the reported symptom, gives the same verdict
every run, takes seconds, and runs unattended.

## 2. Reproduce and minimise
Run the loop and watch it go red with the failure the user reported (not a nearby one), across
several runs; capture the exact symptom. Then cut inputs, callers, config, data and steps one
at a time, re-running after each cut, until every remaining element is load-bearing. The
minimal repro becomes the regression test.

## 3. Hypothesise and test
- Write **3–5 ranked, falsifiable** hypotheses: "if X is the cause, changing Y makes it go
  away / changing Z makes it worse". One you can't phrase that way is dropped or sharpened.
- Show the ranked list before testing (a task: to the lead with SendMessage, without waiting;
  the lead: in `plan.md ## Root cause`). Someone may re-rank it at once.
- Probe one variable at a time, each probe tied to a prediction: a debugger or REPL first,
  else targeted logs at the boundaries that separate hypotheses. Never log everything.
- Tag every debug line with one prefix (`[DEBUG-a4f2]`) so cleanup is a single `rg`.
- Performance: measure a baseline (timer, profiler, query plan) and bisect; logs mislead.

## 4. Fix
- Find the **right seam** for the test first: one where the test exercises the real bug
  pattern as it occurs at the call site.
- Turn the minimised repro into a failing test there and watch it fail. If you forced red by
  changing code or a fixture, `diff` against a pristine copy to prove the change landed.
- Then apply the fix, the smallest one that addresses the confirmed cause.
- No correct seam (only a too-shallow one): that is a finding. Note it (report "Open", or the
  backlog) instead of adding a test that gives false confidence.

## 5. Regression test and cleanup
- The phase-4 test passes, and the phase-1 loop is green on the original, un-minimised scenario.
- No debug instrumentation left: `rg -n 'DEBUG-a4f2'` is empty. Throwaway harnesses are
  deleted, or kept under an obvious debug path.
- The commit body names the cause that turned out true (and the ones ruled out).

## In the ticket workflow
- **Lead, `bug` tickets:** the first task is "build the feedback loop" (`skills:
  [tix:diagnose]`, phases 1–2, and 3 when cheap); its report gives the loop command, the
  redacted red output and the ranked hypotheses. Write `plan.md ## Root cause` from it, then
  plan the fix task (phases 4–5) with the loop command in its `## Checks`.
- **Task agents** whose task file lists `skills: [tix:diagnose]` follow this skill for
  the phases the task names, and report the loop command and its last verdict under Results.

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.
