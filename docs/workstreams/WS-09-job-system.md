# WS-09 · Job System (River)

```
Status: pending
Phase: 2
Depends on: WS-03
Unblocks: WS-17 (billing metering), WS-25 (snapshot scheduling), every async WS
```

## Goal

Stand up the durable job queue. After this WS, any module can register a
worker and queue work that survives restarts, retries on failure, and is
inspectable in an admin UI.

## Scope

**In scope:**
- `internal/app/lahijan/jobs/`:
  - `client.go` — River client setup (uses the existing pgxpool)
  - `supervisor.go` — worker supervisor (graceful start + shutdown)
  - `registry.go` — central registry of job kinds → worker functions
  - `worker.go` — generic worker interface + River `Worker` adapter
- River migrations (golang-migrate format) added to Lahijan's `migrations/`
  dir, paired up + down.
- Job categories with examples:
  - **Metering** (`billing.usage.rollup`) — periodic usage aggregation
    (actually filled in by WS-17; here we just have a no-op example)
  - **Cleanup** (`auditlog.prune`) — periodic audit pruning per retention
  - **Notification** (`notify.email.send`) — async email send (used by WS-06)
  - **Snapshot** (`compute.instance.snapshot`) — placeholder for WS-25
  - **Idempotent webhooks** (Phase 7)
- Admin UI:
  - `GET /api/v1/admin/jobs` — list queued/running/failed jobs
  - `GET /api/v1/admin/jobs/{id}` — detail
  - `POST /api/v1/admin/jobs/{id}/retry` — manually retry a DLQ'd job
  - `POST /api/v1/admin/jobs/{id}/cancel`
- River's built-in web UI is exposed under `/admin/jobs/ui` (admin-only)
- Integration with OpenTelemetry: every job gets its own span; the queue kind
  is a metric.

**Out of scope:**
- Actual domain jobs (those land in their respective modules).
- Multi-instance job dispatch (works out of the box via Postgres; not a
  separate concern).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0008-river-job-queue.md`
- `docs/adr/0009-multi-node-ready.md`
- WS-03 doc (DB layer this WS sits on)
- WS-08 doc (audit: every job start/finish emits an audit event)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- River client wired into `program.Start()`; supervisor shuts down gracefully
- Job registry with at least the no-op example jobs above
- River migrations in our `migrations/` dir
- Admin API for inspecting + retrying + cancelling
- Admin UI mounted at `/admin/jobs/ui`
- OTel spans + metrics on every job execution
- Integration test: queue a job, worker picks it up, completes, observable

## Definition of Done

- [ ] `make dev-up` starts the system with River ready
- [ ] queueing a job survives a process restart (integration test)
- [ ] a failing job retries with exponential backoff, then lands in DLQ
- [ ] DLQ jobs can be retried via the admin API
- [ ] every job execution emits an OTel span
- [ ] admin API requires `platform.admin` permission
- [ ] every privileged admin action emits an audit event
- [ ] `make lint test` green

## Open questions

- Concurrency limits per job kind? (Default: yes, configurable via registry.)
- Periodic scheduling via River's cron-like API, or do we use a separate
  scheduler? (Default: River's periodic jobs.)

## Notes

- River requires its schema in the `lahijan` DB. Add as paired migrations.
- This WS is small but blocking for WS-17 (billing) — schedule accordingly.
