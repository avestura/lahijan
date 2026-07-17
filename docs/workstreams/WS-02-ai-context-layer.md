# WS-02 · AI Context Layer & Docs Framework

```
Status: in-progress
Phase: 0
Depends on: WS-01
Unblocks: WS-03 and every subsequent WS (each loads this layer)
```

## Goal

Make every future AI session self-sufficient. A fresh chat that opens a
workstream doc must be able to understand Lahijan's heart, its settled
decisions, its conventions, and the WS's exact scope — without re-reading
this conversation. This WS produces the load-bearing context layer that makes
that possible.

## Scope

**In scope:**
- `/AGENTS.md` — the project's heart (the 12 pillars, tech stack, repo layout,
  convention summary, the 9-step WS checklist).
- Per-area `AGENTS.md` files:
  - `internal/app/lahijan/AGENTS.md` (backend layering, package map)
  - `web/AGENTS.md` (frontend stack + rules for both `web/` and `website/`)
  - `docs/AGENTS.md` (docs structure + ADR/WS rules)
  - `deployments/AGENTS.md` (compose rules + provider bring-up)
- `opencode.json` at repo root (instructions, skills paths, ws-implementer
  and architect subagents, permission rules).
- Project-specific opencode subagent:
  - `.opencode/agent/ws-implementer.md` — the strict 9-step checklist
    enforcer.
- Project-specific opencode skills:
  - `backend-foundations` (Go conventions, layering, errors, slog, Fiber)
  - `database-conventions` (PostgreSQL + sqlc + golang-migrate + tenant_id)
  - `frontend-foundations` (React + TS + Tailwind + shadcn + i18n)
  - `testing-conventions` (test pyramid, testcontainers, fakes)
  - `adding-a-workstream` (template + workflow for new WS docs)
- ADR framework:
  - `docs/adr/0000-template.md`
  - `docs/adr/README.md` (index)
  - 17 ADRs (0001..0017), one per fork decision in the kickoff conversation.
- Architecture docs:
  - `docs/architecture/overview.md` (component diagram, logical layers,
    tenancy flow, compute flow example, multi-node readiness)
  - `docs/architecture/conventions.md` (the rules, all areas)
  - `docs/architecture/decisions.md` (one-line summary table)
- `docs/glossary.md` — Tenant / User / Instance / Zone / Bucket / etc.
- Workstream framework:
  - `docs/workstreams/_template.md`
  - `docs/workstreams/README.md` (index with statuses + dependency graph)
  - 34 WS docs (Phase 0..7), one per file, each following the template.
- Docusaurus skeleton: `docs-site/` with config + sidebar + package.json so
  `make docs` works.

**Out of scope:**
- Actual backend code beyond fixing the WS-01 case-sensitivity bug.
- Frontend initialization (WS-18).
- Actual Postgres / Incus / PDNS / SeaweedFS wiring.

## Required reading for the AI session

- `/AGENTS.md` (this WS *writes* the file; the next session reads it)
- All 17 ADRs in `docs/adr/` (this WS writes them; subsequent sessions read)

## Deliverables

- [x] `/AGENTS.md`
- [x] `internal/app/lahijan/AGENTS.md`
- [x] `web/AGENTS.md`
- [x] `docs/AGENTS.md`
- [x] `deployments/AGENTS.md`
- [x] `opencode.json`
- [x] `.opencode/agent/ws-implementer.md`
- [x] 5 skills in `.opencode/skills/`
- [x] `docs/adr/0000-template.md` + `docs/adr/README.md` + 17 ADRs
- [x] `docs/architecture/{overview,conventions,decisions}.md`
- [x] `docs/glossary.md`
- [x] `docs/workstreams/{_template.md, README.md}` + 34 WS docs
- [x] `docs-site/` Docusaurus skeleton

## Definition of Done

- [x] A fresh AI session opening any WS doc can list the project's pillars
      without reading this conversation.
- [x] All 17 fork decisions have an ADR.
- [x] All 34 workstreams have a doc with Goal / Scope / Required reading /
      Deliverables / DoD.
- [x] `opencode.json` validates against the schema.
- [x] `make docs` (Docusaurus) builds, even if mostly empty.
- [x] `make lint test` still green.

## Open questions

- (none at start; surface any that arise during execution)

## Notes

This WS is unusually documentation-heavy on purpose. The cost of writing all
this once is paid back every subsequent session. Don't shortcut it.
