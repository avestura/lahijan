---
name: adding-a-workstream
description: "Use when creating a new workstream doc for Lahijan, or when starting a fresh AI session to implement an existing WS. Triggers on 'new workstream', 'add WS', 'create WS', 'implement WS-XX', 'start WS-XX', edits to docs/workstreams/*, workstream template questions."
---

# Adding & Implementing Workstreams

Load this when you create a new workstream doc, or when a fresh AI session is
about to implement WS-XX.

## When ADDING a new workstream doc

1. Copy `docs/workstreams/_template.md` to `docs/workstreams/WS-XX-<name>.md`
   where `XX` is the next available number (check the index).
2. Fill every section. Don't leave `TBD` if you can avoid it — the doc must be
   self-sufficient for a future AI session.
3. Decide **Phase**, **Depends on**, and **Unblocks**. These flow into the
   index's dependency graph.
4. Write the **Goal** paragraph first; it shapes everything else.
5. List **Required reading** conservatively — everything a fresh session
   absolutely must load before writing code. Link to specific ADRs and skills,
   not whole folders.
6. Make the **Definition of Done** concrete and tickable. "Tests added" is
   weak; "≥1 happy-path + ≥1 failure-path test per public function" is strong.
7. Add the WS to `docs/workstreams/README.md` index with status `pending`.
8. Commit: `docs(workstreams): add WS-XX <name>`.

## When IMPLEMENTING a workstream (fresh session)

You MUST follow this exact order. Do not skip steps; do not improvise.

```
1. Read /AGENTS.md end to end.
2. Read docs/workstreams/WS-XX-<name>.md end to end.
3. Read every file in the WS doc's "Required reading" list.
4. Read docs/architecture/conventions.md (especially your area's section).
5. Read the relevant .opencode/skills/<area>/SKILL.md.
6. Branch:  git switch -c feat/ws-XX-<short>
7. Implement one concern at a time, with tests.
8. After each meaningful change:  make lint test
9. Tick every DoD box (or explain why you can't). Update the WS doc's Status.
10. Open a PR with the template checklist ticked.
```

The full checklist lives in `.opencode/agent/ws-implementer.md` — that agent
exists specifically to enforce this.

## What goes in a good WS doc

A good workstream doc is **self-contained**. A fresh AI session that has never
seen this project should be able to:

- Understand the goal without re-deriving it from conversation history
- Know exactly which files to read for context (the "Required reading" list)
- Know what "done" looks like (the DoD checklist)
- Know what's explicitly out of scope (so they don't accidentally do it)

## What does NOT go in a WS doc

- Implementation details (those land in code + ADRs)
- Long prose about "why we chose X" (that's an ADR's job; the WS just links)
- Copies of conventions already in `docs/architecture/conventions.md`
- Things that belong in another WS (link to it instead)

## Anti-patterns to avoid

- "Required reading" with 30+ files — be selective; 5–8 is typical
- DoD boxes that aren't actually checkable ("make it good")
- Scope that overlaps heavily with another WS (split or merge; don't duplicate)
- A Goal that's actually a list of tasks (the Goal is the WHY, not the WHAT)

## Status values (used in the index)

| Status | Meaning |
|--------|---------|
| `pending` | Doc accepted; not started. |
| `in-progress` | Branch exists; PRs flowing. |
| `done` | Merged to main; DoD met. |
| `deferred` | Doc exists; intentionally not in current phase (see Phase 7). |
| `blocked` | Cannot proceed; needs an unblocker (note in doc). |
| `cancelled` | Decided not to do; doc kept for the trail. |

## Example minimal WS doc structure

```markdown
# WS-99 · Example Workstream
Status: pending
Phase: X
Depends on: WS-AA, WS-BB
Unblocks: WS-CC

## Goal
One paragraph.

## Scope
**In scope:**
- thing 1
- thing 2
**Out of scope:**
- thing 3 (see WS-DD)

## Required reading for the AI session
- /AGENTS.md
- docs/adr/0001-example.md
- docs/architecture/conventions.md#<section>
- .opencode/skills/<area>/SKILL.md

## Deliverables
- one
- two

## Definition of Done
- [ ] migrations up + down tested
- [ ] sqlc generate clean
- [ ] ≥1 happy + ≥1 failure test per public function
- [ ] OpenAPI spec updated; clients regenerated
- [ ] ADR written for any new decision
- [ ] make lint test green

## Open questions
- (none)
```
