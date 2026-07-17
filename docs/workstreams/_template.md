# WS-XX · <Workstream Name>

```
Status: pending | in-progress | done | deferred | blocked | cancelled
Phase: <0..7>
Depends on: WS-AA, WS-BB
Unblocks: WS-CC
```

## Goal

One paragraph: **the WHY**, not the what. The reader should understand what
gets better in Lahijan once this WS merges.

## Scope

**In scope:**
- thing 1
- thing 2

**Out of scope** (link to other WS if applicable):
- thing 3 → see WS-DD

## Required reading for the AI session

A fresh AI session that picks up this WS must read these files first:

- `/AGENTS.md`
- `docs/adr/0001-<relevant>.md`
- `docs/architecture/conventions.md#<section>`
- `.opencode/skills/<area>/SKILL.md`
- The relevant area `AGENTS.md` if any (`internal/app/lahijan/AGENTS.md`,
  `web/AGENTS.md`, `docs/AGENTS.md`, `deployments/AGENTS.md`)

## Deliverables

- one
- two

## Definition of Done

- [ ] migrations up + down tested (if DB)
- [ ] `sqlc generate` clean (if DB)
- [ ] ≥1 happy-path + ≥1 failure-path test per public function
- [ ] OpenAPI spec updated; clients regenerated (if API surface changed)
- [ ] ADR written for any new decision
- [ ] `make lint test` green
- [ ] relevant UI page done (if user-facing)
- [ ] relevant `docs/workstreams/WS-XX-*.md` Status field updated
- [ ] PR template checklist ticked

## Open questions

- (any)

## Notes

Optional: implementation sketches, decisions deferred to ADRs, etc.
