# ADR-0005: Single-host topology, multi-host-ready

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan must be deployable by a single self-hoster with `docker compose up`,
but must also be able to scale out for a multi-node cluster later.

Options considered:

- **Single-host only** — simplest; locks us out of future clustering without a
  major refactor.
- **Multi-host from start** — realistic for production; but doubles the
  operational complexity for the self-hoster.
- **Single-host now, multi-host-ready** — ship the simplest possible single-host
  compose, but architect the code (state externalization, idempotent workers,
  shared job queue) so multi-node is a config change, not a rewrite.

## Decision

Lahijan ships a **single-host all-in-one** docker-compose stack for the MVP.
The architecture is **multi-host-ready**:

- All persistent state lives in PostgreSQL (shared), not in process memory.
- The job queue is River (PostgreSQL-native), so multiple Lahijan instances
  can compete for the same work queue.
- Sessions and auth state are DB-backed, not in-memory.
- Provider connections (Incus, PowerDNS, SeaweedFS) are configurable via env,
  so each Lahijan instance can point at any backend.
- Instance placement logic is abstracted behind a `PlacementDriver` interface
  so a future multi-node version can add real scheduling without touching the
  compute module.

## Consequences

- **Positive:** self-hosters get one command to start everything.
- **Positive:** no rewrite needed when scaling out — only config + a Phase 7
  WS (WS-26) wires up the cluster membership.
- **Negative:** every state change must hit the DB, which adds latency.
- **Negative:** the abstraction layer (e.g. `PlacementDriver`) adds a small
  amount of indirection.

## Compliance

- No in-memory caches for state that must survive a restart (use Redis only if
  strictly necessary — currently: never).
- All jobs go through River; no goroutine-based scheduling for durable work.
- `cmd/lahijan` is stateless; running two replicas against the same Postgres
  is safe today.

## References

- ADR-0006 (managed deps — Lahijan ships the whole stack)
- ADR-0007 (shared Postgres)
- ADR-0008 (River)
- ADR-0009 (multi-node-ready — formalizes this commitment)
- WS-23 (production deployment)
- WS-26 (deferred: actual multi-node cluster support)
