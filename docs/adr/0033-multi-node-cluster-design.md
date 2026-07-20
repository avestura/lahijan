# ADR-0033: Multi-node cluster design — PlacementDriver + Incus cluster API

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer

## Context

WS-26 ("Multi-Node / HA Control Plane") makes the architecture in
ADR-0009 ("multi-node-ready from day 1") real. The workstream is small
in scope: it swaps the implicit `LocalPlacementDriver` for a real
`ClusterPlacementDriver` and surfaces the Incus cluster surface to
platform admins. Three forces shape the design:

1. **PlacementDriver abstraction never landed in code.** ADR-0009 names
   the interface; the WS-11 doc says "the MVP `LocalPlacementDriver`
   is implicit (the driver points at one daemon)". In practice the
   compute service calls `provider.CreateInstance` directly; there is
   no place to insert a per-member routing decision. WS-26 must add
   the interface AND ship both implementations without rewriting
   `Service.CreateInstance`.

2. **Incus' cluster API is per-call.** Incus itself is the cluster
   manager — `POST /1.0/instances?project=<p>&target=<member>` is the
   only cluster-aware call needed at create time; live-migrate is
   `POST /1.0/instances/<n>?project=<p>&target=<member>` with
   `migration: true` in the body. There is no separate "cluster
   service" to call. The driver therefore needs `target` plumbing in
   the existing `CreateInstance` call + a new `MigrateInstance` method
   against the existing instances endpoint.

3. **Placement is a per-instance DB row.** Once an instance exists it
   is pinned to a cluster member (its `Location` field on the Incus
   side). The Lahijan row must mirror that so a UI list shows "where
   the instance lives"; the row is reconciled from the daemon's view
   on read so a live-migrate is reflected without a service-layer
   special-case.

Options considered:

- **Option A — Ad-hoc cluster plumbing in the compute service.** Add a
  `target` parameter to `Service.CreateInstance` and a `MigrateInstance`
  method that calls a new `provider.MigrateInstance`. **Pros:**
  minimal new abstraction. **Cons:** the service has cluster-mode
  branching everywhere; tests have to seed two flavours of fake
  provider; future scheduling strategies (bin-packing, spread,
  affinity) have nowhere to plug in.

- **Option B — PlacementDriver interface with two implementations
  (Local + Cluster).** Define `PlacementDriver` in `internal/app/
  lahijan/compute`. The service calls `placement.SelectTarget(params)`
  before every create; the result is the `target` string forwarded
  to `provider.CreateInstance`. `Local` returns "" (Incus treats
  empty target as "any member"); `Cluster` calls `provider.
  ListClusterMembers` + picks the least-loaded. Live-migrate is
  surfaced as a separate service method that the cluster driver
  implements. **Pros:** matches ADR-0009's commitment; the service
  stays scheduler-agnostic; the Local implementation is the zero-cost
  default. **Cons:** one extra interface + one extra struct per
  implementation.

- **Option C — External scheduler (k8s-style controller).** A
  separate process watches instance-create events and rewrites the
  target. **Pros:** matches the "future multi-node" pattern in some
  large cloud stacks. **Cons:** violates pillar 8 ("monolithic Go
  backend — no microservices"); adds an operability burden far
  above the WS-26 scope.

## Decision

Adopt **Option B**. Concretely:

- **`compute.PlacementDriver` interface.** Lives in
  `internal/app/lahijan/compute/placement.go`. The interface carries
  `SelectTarget(ctx, params) (target string, err error)` plus
  `MigrateInstance(ctx, params) error`. The `compute.Service`
  constructor takes a `PlacementDriver` (defaulting to
  `LocalPlacementDriver`); every `CreateInstance` call passes the
  selected target to the provider; the new `MigrateInstance` service
  method delegates to the driver.

- **LocalPlacementDriver.** Returns the empty string for every
  `SelectTarget` call (Incus treats empty target as "any member, any
  daemon"); `MigrateInstance` returns
  `ErrMigrationNotSupported`. This is the implicit behaviour today;
  making it explicit lets `program.Start` always wire a driver and
  the compute service drops its `nil` check.

- **ClusterPlacementDriver.** Calls `provider.ListClusterMembers`,
  picks the least-loaded member (highest free CPU + memory; ties
  broken by name), and returns the member's `ServerName`. The
  decision is wrapped in a Postgres advisory lock keyed on the
  tenant id so concurrent `CreateInstance` calls in different
  replicas do not both pick the same "least loaded" member and
  oversubscribe it.

- **Incus cluster API methods.** Land in
  `internal/app/lahijan/providers/incus/cluster.go`:
  `ListClusterMembers`, `GetClusterMember`, the optional `target`
  parameter plumbed into `CreateInstance`, and `MigrateInstance`
  (`POST /1.0/instances/<n>?target=<member>` with
  `{"migration": true}`). Per pillar 1 these are driver-internal;
  the compute service speaks "compute nodes", not "Incus members".

- **Per-instance placement metadata.** `compute_instances` gains a
  nullable `cluster_member` column. The compute service writes the
  Incus-reported `Location` into it on create + after every
  reconcile + after every migrate. The column is informational; the
  source of truth is the daemon's `Location` field reported by
  `GetInstance`.

- **Cluster admin surface.** New endpoints under
  `/api/v1/admin/compute/cluster/*`: `GET /members`,
  `GET /members/{name}`, `POST /instances/{id}/migrate`. Wired
  behind three new `compute.cluster.*` permissions (platform-only
  by default; granted to `tenant.admin` so a tenant admin can
  rebalance their own instances).

- **Live migration orchestration.** `Service.MigrateInstance` calls
  `provider.MigrateInstance` (which issues the Incus
  `POST /instances/<n>?target=<member>` with the migration body),
  waits for the operation, then updates the `cluster_member` column
  via a reconcile. The migration is stateful: Incus' own operation
  tracker owns durability; the service just records the outcome.

- **Postgres advisory locks for placement.** The advisory lock is
  held for the duration of the Incus `CreateInstance` call so two
  replicas placing instances for the same tenant do not race. The
  lock is keyed on `hashtext(tenant_id::text)` (a 32-bit key; the
  other 32 bits are a constant namespace so future advisory-lock
  consumers in Lahijan do not collide).

## Consequences

- **Positive:** ADR-0009's "no rewrite when scaling out" commitment
  is fulfilled. The compute service has zero new branches for
  cluster vs non-cluster; the driver swap is invisible to handlers,
  repositories, and the audit/event bus seams.

- **Positive:** Future scheduling strategies (bin-packing, spread,
  per-tenant affinity, drain-a-member) live behind the same
  interface — a new driver is one constructor + a config flag.

- **Positive:** Live-migrate is a tenant-admin action; it does not
  require platform-admin privileges. This matches the WS-26 doc's
  "list members, join/evict" admin surface while keeping the
  per-instance operations in the tenant scope.

- **Negative:** The advisory lock adds a Postgres round-trip on every
  create. The cost is sub-millisecond on a healthy cluster; the
  alternative (a race that double-schedules to one member) is worse.

- **Negative:** The `cluster_member` column is denormalised from the
  daemon's `Location` field. A live-migrate done directly via Incus
  (e.g. operator running `incus move`) is not visible until the next
  reconcile. Acceptable — the column is informational.

- **Neutral:** Session affinity is not required (sessions are
  DB-backed per ADR-0009); the load balancer in front of Lahijan can
  use any algorithm. The operator guide documents Caddy + an
  external NLB as the two supported shapes.

## Compliance

- `internal/app/lahijan/compute/placement.go` declares the
  `PlacementDriver` interface, `LocalPlacementDriver`, and
  `ClusterPlacementDriver`.
- `internal/app/lahijan/providers/incus/cluster.go` declares the
  cluster REST surface (`ListClusterMembers`, `GetClusterMember`,
  `MigrateInstance`) and the `target` parameter on `CreateInstance`.
- `internal/app/lahijan/database/migrations/0040_compute_cluster_placement.up.sql`
  adds `compute_instances.cluster_member` (reversible; the down
  migration drops the column).
- `internal/app/lahijan/api/router.go` registers
  `/api/v1/admin/compute/cluster/*` behind `RequirePerm` for the
  three new `compute.cluster.*` permissions.
- `internal/app/lahijan/program/program.go` wires the
  `ClusterPlacementDriver` when `providers.incus.placement.mode ==
  "cluster"`; otherwise it wires the `LocalPlacementDriver`.
- `deployments/README.md` documents the LB topology (Caddy in front
  of N replicas, all pointing at one Postgres) and the Incus
  cluster bootstrap prerequisite.

## References

- WS-26 doc: `docs/workstreams/WS-26-multi-node-cluster.md`
- ADR-0005 (single-host now)
- ADR-0008 (River makes multi-instance safe)
- ADR-0009 (multi-node-ready — the commitment this WS fulfils)
- ADR-0010 (full Incus surface)
- ADR-0030 (production deployment topology — single-host compose)
- [Incus clustering](https://linuxcontainers.org/incus/docs/main/explanation/clustering/)
