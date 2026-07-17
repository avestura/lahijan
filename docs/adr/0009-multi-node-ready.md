# ADR-0009: Multi-node-ready from day 1

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan's MVP target is a single host (ADR-0005), but the project's stated goal
includes multi-node scale-out for larger deployments. If the codebase commits
to single-node assumptions, the multi-node refactor later will be enormous.

Options considered:

- **Single-node lock-in** — simplest; refactor later if needed.
- **Design for multi-node from day 1** — pay a small ongoing cost in
  abstraction discipline so the multi-node story is a config + clustering WS,
  not a rewrite.

## Decision

Lahijan's architecture is **multi-node-ready** from day 1:

- **No in-process state that must survive a restart.** Auth, sessions, jobs,
  audit, billing — all in PostgreSQL.
- **No goroutine-based durable scheduling.** All durable work goes through
  River (ADR-0008), which is shared across instances.
- **Instance placement is abstracted.** The compute module calls a
  `PlacementDriver` interface; the MVP implementation is `LocalPlacementDriver`
  (talks to the local Incus socket). WS-26 swaps in a `ClusterPlacementDriver`
  without touching the compute module.
- **Configurable backend endpoints.** Incus/PDNS/SeaweedFS hosts come from
  config, not from compile-time assumptions.
- **Idempotent workers.** Any River worker that runs twice on the same job
  produces the same result.

## Consequences

- **Positive:** WS-26 (multi-node cluster) is achievable without rewriting
  existing modules.
- **Positive:** running two replicas of Lahijan against the same Postgres is
  already safe — useful for blue/green deploys.
- **Negative:** every state change pays a DB round-trip.
- **Negative:** developers must be disciplined about avoiding in-process
  caches for state that should be shared.

## Compliance

- No package-level `var` holds mutable state that other requests depend on
  (except registries set at startup).
- Every long-running task is a River job.
- Every backend connection is configurable via `conf`.

## References

- ADR-0005 (single-host now)
- ADR-0008 (River makes multi-instance safe)
- ADR-0010 (Incus surface — placement driver abstraction)
- WS-26 (deferred: real multi-node support)
