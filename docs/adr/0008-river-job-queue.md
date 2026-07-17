# ADR-0008: River (PostgreSQL-native) for jobs

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Several features need asynchronous, durable work: instance start/stop, DNS
zone propagation, snapshot scheduling, WASM plugin host calls, metering rollup,
audit log archival. We need a job queue.

Options considered:

- **In-process `gocron`** — simple; lost on restart; not durable; no retry.
- **Asynq (Redis)** — proven; adds a Redis dependency; contradicts ADR-0005
  (we minimize stateful services).
- **Temporal** — heavyweight workflow engine; overkill for MVP; steep learning curve.
- **River (PostgreSQL-native)** — durable; uses existing Postgres; Go-first;
  no new deps; small surface area.

## Decision

Use **River** for all durable asynchronous work. River's tables live in the
`lahijan` Postgres database as the `river_*` schema.

River provides:

- Durable queues (survive restarts)
- Automatic retries with exponential backoff
- Dead-letter queue
- Scheduled jobs
- Web UI for inspection

## Consequences

- **Positive:** one fewer service to run (no Redis).
- **Positive:** jobs are transactional with the data they affect (same DB).
- **Positive:** multiple Lahijan replicas can compete for work safely (sets up
  multi-node per ADR-0005).
- **Negative:** high-throughput workloads may stress Postgres; acceptable for
  MVP. If we ever need to offload, write a new ADR.
- **Negative:** River is newer than Asynq/Temporal; smaller ecosystem.

## Compliance

- `internal/app/lahijan/jobs/` contains the River client setup, worker
  supervisor, and the job registry.
- Every durable async task is a River job, not a goroutine.
- A `jobs.JobArgs` interface is defined per area; workers live with their area.
- River migrations are paired with Lahijan migrations.

## References

- [River documentation](https://riverqueue.com/)
- ADR-0003 (PostgreSQL)
- ADR-0005 (multi-host-ready — needs durable queue)
- WS-09 (job system)
