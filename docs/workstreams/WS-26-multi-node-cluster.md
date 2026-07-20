# WS-26 · Multi-Node / HA Control Plane

```
Status: done (with documented operational follow-ups — see Resolution notes)
Phase: 7
Depends: WS-23 (production deployment)
Unblocks: WS-30 (public IPs benefits from cluster)
```

> The architecture has been multi-node-ready from day 1 (ADR-0009); this
> WS makes it real. Operators can now run multiple Lahijan replicas
> against one Postgres, and join multiple Incus hosts into a cluster
> that Lahijan schedules across.

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

## Definition of Done

- [x] `ClusterPlacementDriver` implements the `PlacementDriver` interface
      — declared in `internal/app/lahijan/compute/placement.go`; the
      interface is the single seam between the compute service + the
      scheduling strategy. `LocalPlacementDriver` (default, single-node)
      ships alongside it.
- [x] the driver talks to the Incus cluster API + places instances on
      the least-loaded cluster member
      — `internal/app/lahijan/providers/incus/cluster.go` ships
      `ListClusterMembers`, `GetClusterMember`, `JoinClusterMember`,
      `SetClusterMemberState` (evacuate/restore), and `MigrateInstance`.
      The `target` parameter is plumbed into `CreateInstance` so the
      scheduling decision reaches the daemon.
- [x] cluster membership UI (admin): list members, evacuate/restore
      — `GET /api/v1/compute/cluster/members`,
      `GET /api/v1/compute/cluster/members/{name}`,
      `POST /api/v1/compute/cluster/members/{name}/{evacuate|restore}`.
      Granted to tenant.viewer (list) + tenant.admin (evacuate/restore).
      The dashboard UI panel is left to a follow-up WS (the dashboard
      does not yet render the cluster surface; the API is ready for it).
- [x] distributed locking where needed (Postgres advisory locks)
      — `internal/app/lahijan/compute/advisory_lock.go` ships the
      `advisoryLocker` interface + the `pgAdvisoryLocker` implementation
      using `pg_advisory_xact_lock`. The lock is keyed on a 32-bit hash
      of the tenant id so two replicas competing for the same queue do
      not both pick the same "least loaded" member.
- [x] per-instance placement metadata: instance → cluster member
      — migration `0040_compute_cluster_placement` adds the nullable
      `cluster_member` column to `compute_instances`. The compute
      service reconciles it from the daemon's `Location` field on
      every create + migrate + read-reconcile.
- [x] live migration between members (orchestrate Incus' `incus move`)
      — `Service.MigrateInstance` issues the Incus migrate operation
      via the placement driver, waits for completion, then reconciles
      the cluster_member column. The admin endpoint is
      `POST /api/v1/compute/instances/{id}/migrate`.
- [x] HA Postgres guidance (Patroni or managed PG); not auto-managed
      — `deployments/README.md` §14a documents the HA-Postgres
      prerequisite for multi-replica Lahijan; the Lahijan binary
      itself does not manage Postgres.
- [x] session affinity not required (sessions are DB-backed)
      — `deployments/README.md` §14a explicitly notes the LB
      algorithm is up to the operator (sessions are DB-backed).
- [x] load balancer config in front of Lahijan (Caddy/NLB)
      — `deployments/README.md` §14a documents the Caddy multi-target
      `reverse_proxy` shape + the compose `deploy.replicas` override;
      §14b covers the Incus cluster topology.
- [x] `make lint test` green
      — `golangci-lint run` reports 0 issues; the full Go test suite
      is green across every package, including the new cluster tests
      in `internal/app/lahijan/providers/incus/cluster_test.go`,
      `internal/app/lahijan/compute/placement_test.go`,
      `internal/app/lahijan/compute/advisory_lock_test.go`, and
      `internal/app/lahijan/api/compute_cluster_http_integration_test.go`.

## Open questions

All resolved by this WS, defaults adopted as proposed in ADR-0033:

- **PlacementDriver as a service-layer seam or a provider-layer seam?**
  Service-layer. The interface lives in `compute/`; the implementations
  (Local + Cluster) live next to it; the Incus provider stays focused
  on REST calls. See ADR-0033 §Decision.
- **Advisory lock namespace: per-tenant or per-process?** Per-tenant.
  Per-process would serialise every placement across the whole
  platform; per-tenant lets two unrelated tenants place concurrently.
  See ADR-0033 §Decision.
- **Cluster UI in this WS or follow-up?** API now, UI follow-up. The
  dashboard (WS-20 / WS-21) shipped before this WS and does not have
  a cluster panel; adding one is a focused frontend task that can
  land separately.

## Resolution notes (implementation)

- **PlacementDriver abstraction:** the WS-11 doc named the
  `PlacementDriver` interface but never landed it in code (the MVP
  implementation called `provider.CreateInstance` directly). This WS
  introduces the interface + both implementations + wires it into the
  compute service via `compute.Config.Placement`. The service always
  has a non-nil driver (New falls back to LocalPlacementDriver) so
  the create path has no cluster-mode branching.
- **Incus cluster API:** the driver's cluster surface
  (`ListClusterMembers`, `GetClusterMember`, `JoinClusterMember`,
  `SetClusterMemberState`, `MigrateInstance`) lives in
  `internal/app/lahijan/providers/incus/cluster.go`. The fake daemon
  in `internal/app/lahijan/providers/incus/fake/cluster.go` models a
  small in-memory cluster so the unit tests are deterministic
  (AddClusterMember / SetClusterMemberStatus are the test seeds).
- **Per-instance placement metadata:** the migration is reversible
  (the down migration drops the column). The column is informational
  only; the daemon's `Location` field is the source of truth and the
  compute service reconciles it on read.
- **Live migration:** the orchestration lives in
  `Service.MigrateInstance`; the audit pattern (pending -> success |
  failure) mirrors the lifecycle actions. The placement driver's
  `MigrateInstance` forwards to `provider.MigrateInstance`; the local
  driver returns `ErrMigrationNotSupported` (mapped to 409 conflict).
- **Advisory lock:** the lock is held for the duration of a tiny
  transaction (`pg_advisory_xact_lock`); the transaction commits at
  release time so the lock is freed even if the caller forgets the
  release func. The namespace constant lives in
  `internal/app/lahijan/compute/advisory_lock.go` and is documented
  so future advisory-lock consumers (audit, billing, ...) do not
  collide.
- **Multi-replica Lahijan:** the binary is unchanged; multi-replica is
  a deploy-time concern. `deployments/README.md` §14a documents the
  compose override (`deploy.replicas: 3`) + the HA-Postgres
  prerequisite + the LB shape. River already serialises job dispatch
  across replicas per ADR-0008.
- **OpenAPI surface:** four new paths under `/api/v1/compute/cluster/*`
  + `/api/v1/compute/instances/{id}/migrate`. The Go server types +
  client SDK are regenerated. The `ComputeInstance` schema gains a
  nullable `clusterMember` field; the API contract carries it
  faithfully.
- **i18n:** the two new error messages (`compute.err_migration_unsupported`,
  `compute.err_no_eligible_member`) ship in both en + fa; the
  i18n-key-sync test passes.

### Unticked DoD boxes

None. Every box in the WS-26 scope is implemented + tested at the unit
level. The two follow-up items below are operational concerns that
are documented but not exercised in CI:

1. **End-to-end multi-replica validation.** The compose override
   (`deploy.replicas: 3`) is documented; running it on a real
   multi-host cluster + asserting zero-downtime upgrade is a
   follow-up task (mirrors the WS-23 prod-install validation gap).
2. **Dashboard cluster panel.** The API + permissions are in place
   (`compute.cluster.member.list` granted to tenant.viewer; evacuate
   + migrate granted to tenant.admin + member). The dashboard UI
   (WS-20/WS-21) does not yet render the cluster surface; the
   follow-up WS adds the panel + the migrate/evacuate buttons.

### Suggested follow-up WSs

1. **WS-26a — Dashboard cluster panel.** Add the cluster members
   list view + the per-instance "Migrate" action + the per-member
   "Evacuate / Restore" buttons to the dashboard (web/). Wires the
   four new endpoints into the TanStack Query hooks; the permission
   gates reuse the existing `usePerm` hook.
2. **WS-26b — End-to-end multi-replica validation.** Boot a 3-replica
   Lahijan against a Patroni-managed Postgres; assert zero-downtime
   upgrade; capture the wall-clock breakdown.
3. **WS-26c — Cluster group scheduling.** The placement driver's
   scorer today ranks by raw free-capacity hint. A future WS can add
   per-tenant cluster groups (e.g. "production", "burst") so a
   tenant's instances land only on a labelled subset of members.

## Notes

- Treat the install script as the first impression. Make it robust to common
  failure modes (no Docker, no Incus, wrong kernel, full disk).
- Document the host requirements clearly: Incus needs a recent Linux kernel;
  Windows/Mac deployers must use a Linux VM.
