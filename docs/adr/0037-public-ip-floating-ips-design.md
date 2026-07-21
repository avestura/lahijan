# ADR-0037: Public IP Assignment & Floating IPs design (WS-30)

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer

## Context

WS-30 ("Public IP Assignment & Floating IPs") was deferred past MVP because the
MVP topology (ADR-0005) uses Incus bridge networking + optional proxy devices,
which is enough for self-hosters. WS-30 is the SaaS-style surface:

1. **Operator-owned IP pools** — the operator registers the public IPv4 + IPv6
   ranges they own (RIR-allocated or provider-assigned). Lahijan records the
   pool as the source of allocatable addresses.
2. **Per-tenant floating IPs** — a tenant allocates one of the operator's
   addresses; that address is "the tenant's" until the tenant releases it.
3. **Attach / detach** to instances — a floating IP forwards traffic to a
   specific instance. Detaching keeps the IP allocated to the tenant; the
   instance just loses the public address.
4. **Reverse-DNS auto-publish** — when a floating IP is allocated (or its
   PTR target changes), Lahijan publishes the PTR record into the
   corresponding `in-addr.arpa` / `ip6.arpa` zone via the DNS module (WS-15).
5. **Per-IP-hour billing** — every allocated floating IP emits a usage event
   per minute (same cadence as the compute metering) priced via the catalog.
6. **Audit + WASM events** — every privileged action (`compute.ip.assigned`,
   `compute.ip.released`) is audited and fanned into the WASM event bus so
   plugins can react.

The WS-30 doc itself carries a heavy operational caveat: *"This WS is
operationally heavy (BGP, RIR allocation, abuse handling). Most self-hosters
won't need it; it's primarily for SaaS-style deployments."* This ADR's central
decision is the line between what Lahijan owns (the control plane) and what
the operator owns (the data plane).

Three sub-decisions land here.

### Sub-decision A — Source of truth + propagation

Options considered:

- **Option A1 — Incus network forwards as source of truth; Lahijan
  reads-through.** Pros: zero drift. Cons: every `GET` endpoint becomes a
  daemon round-trip; no place to hang tenant-scoped audit; the operator's
  pool of addresses is not even visible to Incus (Incus only knows the
  forwards already created).
- **Option A2 — Postgres as source of truth; Lahijan pushes a network
  forward (or proxy device) to Incus on every attach/detach.** Pros:
  matches the existing versioning / lifecycle / quota pattern (WS-13, WS-29);
  control plane stays fast; tenant-scoped audit fits. Cons: drift is
  possible if an operator hand-edits Incus. Mitigation: a future reconcile
  worker can re-converge.
- **Option A3 — Hybrid: Postgres for the pool + allocations; Incus for the
  live forward state.** Same drawback as A1 for the live state.

### Sub-decision B — How traffic actually reaches the instance

Two plumbing options exist; both are valid Incus configurations:

- **Option B1 — Network forward.** A `networks/<name>/forwards` entry maps
  the public listen address to a target instance address + ports. Works
  only when the network is a managed bridge with the public IP plumbed on
  the host.
- **Option B2 — Proxy device.** A per-instance `proxy` device binds the
  public IP on the host and forwards TCP/UDP to the instance. Works without
  a managed bridge; the operator must route the public IP to the host.

Lahijan does NOT try to pick: the operator configures the data plane
externally (BGP via FRR, static route, IGP, ...). Lahijan only records the
allocation in Postgres and pushes a network forward when one is configured.
When the operator's topology does not allow a forward (the common case for
self-hosters, who don't have BGP), the allocation still lands and the
operator's automation handles plumbing.

### Sub-decision C — Reconcile-on-read vs. lazy reconcile

Same question as WS-29: do we round-trip to Incus on every `GET`, or cache
in Postgres and trust the cached row?

Options considered:

- **Option C1 — Read-through on every `GET`.** Pros: always-fresh UI.
  Cons: per-`GET` daemon round-trip; the daemon does not even know about
  pool addresses that are allocated-but-not-yet-attached.
- **Option C2 — Cache in Postgres; the row is the truth.** Pros: matches
  WS-29 + the rest of the compute / storage modules; the operator's
  data-plane state is their own concern anyway.

## Decision

WS-30 implements all three sub-decisions as follows.

### A — Postgres is the source of truth; push to Incus opportunistically

- Four new tables:
  - `ip_pools` (operator-owned, **no tenant_id** — the pool is global).
  - `ip_pool_ranges` (per-pool CIDR ranges; v4 + v6 supported).
  - `ip_pool_addresses` (one row per allocatable IP, expanded from a range
    at add time; carries the current state: available / allocated /
    maintenance).
  - `floating_ips` (**tenant-scoped**; carries the allocated address, the
    ptr_target, and the optional instance_id it is currently attached to).
- Every privileged action: validate → audit pending → write Postgres →
  push a network forward (or proxy device) to Incus when the floating IP
  is attached → event-bus emit → audit outcome. This is the same shape as
  `Service.SetBucketVersioning` and the WS-29 lifecycle surface.
- A failed Incus push does NOT roll back the allocation — the operator may
  be using external plumbing. The push is best-effort; the audit row
  records the push outcome so the operator can see whether Lahijan
  attempted the forward.

### B — Network forward when available; operator owns the data plane

- When a floating IP is attached, the compute service attempts to push a
  network forward via `provider.CreateNetworkForward`. The forward maps
  the public address to the instance's primary address.
- The forward name encodes the floating IP id so detach can remove the
  exact forward.
- If the operator's topology does not allow forwards (single-node daemon
  with no managed bridge, no public IP plumbed on the host), the push
  fails; the allocation stays. The operator's external automation (BGP /
  FRR / static route) handles the actual plumbing.
- The Incus-specific concerns (proxy device, network forward) live entirely
  in the Incus provider driver and in the compute service. End users see
  only "compute floating IP" (pillar 1).

### C — Cache in Postgres; no daemon round-trip on read

- Floating IP state is written through to Postgres on every attach /
  detach and read from Postgres on every Get. No reconcile worker is
  added in this WS — the operator's data plane is their own concern, so a
  drift between Postgres and the live Incus forward is a deployment-
  specific problem a future WS can address with a reconcile-on-tick.

### What ships in this WS vs. follow-up

- **In scope (this WS):** operator pool + range + address management;
  tenant floating IP allocate / list / get / attach / detach / release;
  reverse-DNS auto-publish (PTR) into the corresponding in-addr.arpa zone
  when the tenant owns it; per-IP-hour billing usage emission; WASM
  events `compute.ip.assigned` / `compute.ip.released`; audit + RBAC +
  OpenAPI surface; tests at every layer.
- **Deferred to follow-up WSs:**
  - **UI** (pool admin, per-tenant allocation list, attach/detach on the
    instance detail page). The WS-30 doc's "UI" bullet is one line; the
    dashboard work is its own follow-up because it needs forms,
    validation, and i18n in two locales. The backend ships the full
    control-plane surface; the dashboard can consume it as-is.
  - **BGP / FRR / NAT enforcement.** Operator concern. Documented in
    `deployments/README.md`; Lahijan does not manage the data plane.
  - **Per-IP rate limiting + abuse handling.** Separate concern; needs
    host-level enforcement (xtables, nftables, or an external IDMS).
  - **Egress metering.** Needs data-plane integration (NetFlow / IPFIX
    from the host). The per-IP-hour charge ships now; per-GB egress
    ships later.
  - **Reconcile worker.** A periodic River worker that re-pushes the
    Postgres state to Incus on every tick. Not needed yet — the operator
    is the only one who can hand-edit Incus, and they are also the one
    who decided to do so.

## Consequences

- **Positive:** Postgres is the source of truth → tenant-scoped audit
  fits naturally; the pool's free/allocated state is a `SELECT count(*)`
  away; the operator UI renders without a per-row daemon round-trip.
- **Positive:** the WASM event surface (`compute.ip.assigned`,
  `compute.ip.released`) gives plugins a uniform hook for chargeback
  workflows, geofencing, or notification pipelines.
- **Positive:** the operator can run WS-30 without BGP — the data plane
  is optional. A self-hoster with one public IP can use the surface for
  tracking + DNS without ever plumbing the forward.
- **Negative:** Postgres and Incus can drift if an operator hand-edits
  the daemon's forwards. Mitigation: future WS can add a reconcile worker
  (same shape as the WS-29 lifecycle evaluator).
- **Negative:** the per-IP-hour charge relies on the existing metering
  cadence (once per minute); a sub-minute burst of attach/detach cycles
  produces at most one minute of charge per cycle. Acceptable for IP
  allocations, which are slow-changing.
- **Negative:** the operator must register their public IP ranges before
  tenants can allocate. A self-hoster who does not own a range cannot use
  the surface — that's by design (the surface is SaaS-style).

## Compliance

- Migration `0048_compute_ip_pools_floating_ips` adds the four tables;
  the down migration drops them in reverse order. The migration is
  reversible and the CI up/down/up ladder is green.
- `internal/app/lahijan/database/queries/compute_ip_pools.sql` +
  `compute_floating_ips.sql` add the sqlc queries; repo wrappers live in
  `database/ippools_repo.go` + `database/floatingips_repo.go`.
- `internal/app/lahijan/compute/ip_pools.go` + `floating_ips.go` add the
  service layer (privileged-action orchestration + WASM event emission +
  best-effort Incus forward push + reverse-DNS auto-publish).
- Audit actions `compute.ip_pool.create` / `.update` / `.delete` /
  `compute.floating_ip.allocate` / `.attach` / `.detach` / `.release` are
  added to `auth/audit/audit.go`.
- RBAC permissions `compute.ip_pool.manage` (operator-only) +
  `compute.floating_ip.manage` + `compute.floating_ip.read` are added to
  `auth/rbac/permissions.go` and gated by the api/middleware RequirePerm
  seam.
- WASM event topics `ComputeIPAssigned` / `ComputeIPReleased` are added
  to `wasm/eventbus/events.go`.
- OpenAPI schemas (`ComputeIPPool`, `ComputeIPPoolRange`,
  `ComputeFloatingIP`) and endpoints (`/api/v1/compute/ip-pools*`,
  `/api/v1/admin/compute/ip-pools*`, `/api/v1/compute/floating-ips*`) are
  added; Go server + Go client SDK + TS schema regenerated.
- Per pillar 1: the user-facing terms are "floating IP" and "IP pool".
  Incus, BGP, FRR are never named in the API or UI.
- Per pillar 2: `floating_ips` carries `tenant_id NOT NULL`. `ip_pools`,
  `ip_pool_ranges`, `ip_pool_addresses` are operator-owned global tables
  (they appear in the glossary's global-tables row alongside `tenants`,
  `users`, ...).

## References

- ADR-0002 (tenancy — every tenant-scoped table carries tenant_id)
- ADR-0009 (multi-node-ready — WS-30 benefits from cluster but works
  single-node too)
- ADR-0010 (full Incus surface — network forwards + proxy devices are
  part of the Incus surface Lahijan wraps)
- ADR-0019 (append-only audit outcomes)
- WS-11 (Incus provider), WS-14 (compute module), WS-15 (DNS module —
  reverse-DNS auto-publish), WS-17 (billing & metering — per-IP-hour),
  WS-26 (multi-node cluster — WS-30 unblocks nothing but benefits from)
- [Incus network forwards](https://linuxcontainers.org/incus/docs/main/howto/network_forwards/)
- [Incus proxy device](https://linuxcontainers.org/incus/docs/main/reference/devices_proxy/)
