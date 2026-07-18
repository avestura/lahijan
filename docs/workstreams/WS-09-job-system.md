# WS-09 · Job System (River)

```
Status: done
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

- [x] `make dev-up` starts the system with River ready
- [x] queueing a job survives a process restart (integration test)
- [x] a failing job retries with exponential backoff, then lands in DLQ
- [x] DLQ jobs can be retried via the admin API
- [x] every job execution emits an OTel span
- [x] admin API requires `platform.admin` permission
- [x] every privileged admin action emits an audit event
- [x] `make lint test` green

## Open questions

Both resolved by this WS, defaults adopted as proposed:

- **Concurrency limits per job kind?** Yes, configurable via the registry:
  `KindSpec.Concurrency` overrides the per-queue MaxWorkers for the kind's
  queue. Zero (the common case) inherits the queue's MaxWorkers.
- **Periodic scheduling via River's cron-like API, or do we use a separate
  scheduler?** River's periodic jobs. The supervisor wires
  `river.Config.PeriodicJobs` and the registry will gain a
  `RegisterPeriodic` helper when the first domain periodic job lands
  (WS-17 will be the first consumer).

## Notes

- River requires its schema in the `lahijan` DB. The schema is installed
  by migrations `0018_river_schema_base` (which carries the
  `ALTER TYPE ... ADD VALUE 'pending'` as its LAST statement so Postgres
  commits the new value before any constraint change tries to use it)
  and `0019_river_schema_rest` (the constraint change + later upstream
  migrations + the marker rows in `river_migration`). Both apply cleanly
  forward and reverse; the reversibility test in
  `database/migrations_integration_test.go` is updated to assert at
  least version 19.
- This WS is small but blocking for WS-17 (billing) — schedule accordingly.
- River's `Client.Start` ties the client's lifetime to the supplied
  context: a timeout context stops the workers after the timeout. The
  supervisor and `program.Start` therefore pass `context.Background()` and
  rely on `Stop` for graceful drain.
