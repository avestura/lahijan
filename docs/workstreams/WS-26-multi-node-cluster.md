# WS-26 · Multi-Node / HA Control Plane (DEFERRED)

```
Status: deferred
Phase: 7
Depends: WS-23 (production deployment)
Unblocks: WS-30 (public IPs benefits from cluster)
```

> **Deferred past MVP.** The architecture is multi-node-ready from day 1
> (ADR-0009); this WS makes it real.

## Goal

Let operators run multiple Lahijan instances against one Postgres, and join
multiple Incus hosts into a cluster, so the platform is HA and horizontally
scalable.

## Scope (when work begins)

- `ClusterPlacementDriver` — implements the `PlacementDriver` interface from
  WS-11; talks to Incus cluster API; places instances on the least-loaded
  cluster member
- Cluster membership UI (admin): list members, join/evict
- Distributed locking where needed (Postgres advisory locks via River)
- Per-instance placement metadata: instance → cluster member
- Live migration between members (orchestrate Incus' `incus move`)
- HA Postgres guidance (Patroni or managed PG); not auto-managed by Lahijan
- Session affinity not required (sessions are DB-backed)
- Load balancer config in front of Lahijan (Caddy/Traefik in compose; or
  external NLB)

## Required reading (when work begins)

- `/AGENTS.md`
- `docs/adr-0005-mvp-topology.md`
- `docs/adr-0009-multi-node-ready.md`
- `docs/adr-0008-river-job-queue.md` (jobs already multi-instance safe)
- WS-09, WS-11, WS-14, WS-23 docs
- [Incus clustering](https://linuxcontainers.org/incus/docs/main/explanation/clustering/)

## Notes

- This WS mostly swaps `LocalPlacementDriver` for `ClusterPlacementDriver`;
  the rest is operational guidance.
- River already handles multi-instance job dispatch — no work there.
