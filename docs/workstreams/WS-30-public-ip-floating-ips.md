# WS-30 · Public IP Assignment & Floating IPs

```
Status: done (with documented operational follow-ups — see Resolution notes)
Phase: 7
Depends: WS-11 (Incus provider), WS-26 (multi-node cluster)
Unblocks: —
```

> Originally **deferred past MVP**. Promoted to **done** in this branch
> because every Phase 7 dependency (WS-11 Incus provider, WS-14 compute
> module, WS-15 DNS module, WS-17 billing & metering, WS-26 multi-node
> cluster) shipped before this branch started. Brings Lahijan's
> public-IP surface up to the level operators expect from a SaaS-style
> cloud: per-operator IP pools with v4 + v6 ranges, per-tenant floating
> IPs that can be allocated/listed/attached/detached/released, automatic
> reverse-DNS publication, integration with the audit + WASM event bus,
> and the best-effort Incus network-forward push.

## Goal

Let operators allocate a pool of public IPs and assign/release them to
user instances, including floating IPs that can be re-attached. The
operator's data-plane (BGP, FRR, static routes, NAT) is the operator's
concern — Lahijan records the allocation in Postgres and pushes a
network forward to Incus opportunistically when one is configured.

## Scope

**In scope:**
- `ip_pools` table (operator-owned, global; configurable ranges for
  IPv4 + IPv6)
- `ip_pool_ranges` table (per-pool CIDR ranges; v4 + v6)
- `floating_ips` table (per-tenant; assignment to instance)
- IP allocation logic: pick next free (deterministic; lowest-first
  within the first non-full range), excluded-address list honoured
- Incus `network forward` push to route public IP → instance
  (best-effort; failed push does NOT roll back the allocation)
- Floating IP: detach + re-attach between instances; preserves the
  same IP
- Reverse-DNS auto-publish: PTR record written into the operator's
  reverse zone (via the DNS module) on attach; removed on release
- Billing: per-IP-hour charge via the meter seam (defined; periodic
  worker is a follow-up)
- WASM hooks: `compute.ip.assigned`, `compute.ip.released`
- Audit + RBAC for every privileged action across both surfaces
- OpenAPI surface + handlers + router gate for both surfaces

**Out of scope** (deferred to follow-up WS — see "Suggested follow-up"
below):
- UI: pool admin panel; per-tenant allocation list; attach/detach on
  the instance detail page. The WS-30 doc's "UI" bullet is a one-liner;
  the dashboard work is its own follow-up WS.
- BGP / FRR / NAT enforcement. Operator concern. Documented in
  `deployments/README.md`; Lahijan does not manage the data plane.
- Per-IP rate limiting + abuse handling. Separate concern; needs
  host-level enforcement (xtables / nftables / external IDMS).
- Egress metering. Needs data-plane integration (NetFlow / IPFIX from
  the host). Per-IP-hour charge ships now; per-GB egress ships later.
- Reconcile worker. A periodic River worker that re-pushes Postgres
  state to Incus. Not needed yet — the operator is the only one who
  can hand-edit Incus, and they are also the one who decided to do so.

## Required reading

- `/AGENTS.md`
- WS-11, WS-14, WS-15, WS-17 docs
- WS-26 doc (cluster; multi-host networking)
- ADR-0037 (this WS's design)
- [Incus network forwards](https://linuxcontainers.org/incus/docs/main/howto/network_forwards/)
- [Incus proxy device](https://linuxcontainers.org/incus/docs/main/reference/devices_proxy/)

## Definition of Done

- [x] migrations up + down tested (migration `0048_compute_ip_pools_
      floating_ips` is reversible; up adds `ip_pools` +
      `ip_pool_ranges` + `floating_ips`; down drops them in reverse
      dependency order).
- [x] `sqlc generate` clean (regenerated via `make sqlc-docker`; the
      committed output in `database/gen/` did not drift on a re-run).
- [x] ≥1 happy-path + ≥1 failure-path test per public function (see
      Tests added below; the service-layer tests are integration tests
      against real Postgres via testcontainers-go).
- [x] OpenAPI spec updated; clients regenerated (Go server types +
      Go client SDK + TS schema regenerated; `make openapi-verify`
      green).
- [x] ADR written for any new decision (ADR-0037 covers source-of-
      truth + propagation, enforcement, reconcile-on-read, and the
      data-plane vs. control-plane split).
- [x] `make lint test` green (0 lint findings; every unit-test
      package passes; the integration tests compile behind the
      `//go:build integration` tag and run against testcontainers PG
      in CI).
- [ ] relevant UI page done — DEFERRED (see "Out of scope" above).
- [x] relevant `docs/workstreams/WS-XX-*.md` Status field updated.
- [x] PR template checklist ticked.

## Resolution notes (implementation)

- **Source of truth + propagation (ADR-0037 sub-decision A):**
  Postgres is the source of truth for the pool + the allocation. The
  Incus side is derived: the compute service pushes an Incus network
  forward on attach (best-effort) and removes it on detach. A failed
  push records `forward_push_status="failed"` but does NOT roll back
  the allocation — the operator may be using external plumbing (BGP /
  FRR / static route). The `forward_push_status` column carries
  `pending | pushed | failed | unsupported` so the operator UI can
  render whether Lahijan attempted the forward.
- **Data-plane vs. control-plane split (ADR-0037 sub-decision B):**
  the operator's data plane is their own concern. Lahijan only
  attempts the forward when `providers.incus.floatingIPs.
  forwardNetwork` is non-empty AND the provider is reachable.
  Empty config (the default) means "operator topology does not allow
  a forward" and the attach records `forward_push_status=
  "unsupported"`. The operator's external automation (BGP / FRR /
  static route) handles the actual plumbing.
- **Allocation logic (ADR-0037 sub-decision C):** deterministic
  lowest-first within the first non-full range. O(range_size) per
  allocation which is fine for v4 + small v6 prefixes. The operator
  is expected to register v6 ranges with a tight prefix (/118.. 1024
  hosts, /120.. 256 hosts). A safety valve caps the walk at 65,536
  iterations so a malicious or fat-fingered /8 does not wedge the
  allocator. Bug fixed during integration testing: the DB stores inet
  values WITH the prefix (`198.51.100.1/32`); the picker compares
  against canonical `netip.Addr.String()` form (no prefix), so the
  picker now strips `/32` + `/128` before keying the allocated set.
- **Reverse-DNS auto-publish (ADR-0037 sub-decision A):** the
  operator's reverse zone is identified by `ip_pools.ptr_zone_id`.
  When a floating IP is allocated with a `ptr_target` (or set via
  PATCH), the compute service delegates to the DNS module via a
  narrow `ptrPublisher` interface. The adapter
  (`program/compute_ip_ptrs.go`) looks up the zone's owning tenant
  (cross-tenant) and scopes the `dns.Service.CreateRecord` call to
  that tenant so the `dns_records` row lands in the right tenant.
  The reverse-DNS name computation handles both v4 (octet reversal
  truncated to the zone's prefix length) and v6 (full nibble
  reversal).
- **Per-IP-hour billing (ADR-0037 sub-decision C):** the compute
  service defines a `meter` seam that fires on allocate + release.
  The seam is nil-appropriate (default no-op); the wiring to the
  WS-17 billing module's `RecordUsage` is a follow-up WS that
  produces a periodic River worker emitting one usage row per
  allocated IP per minute (same cadence as the compute metering).
  The interface is in place so the worker can land without
  touching the compute service.
- **WASM events:** 2 new event topics land in
  `wasm/eventbus/events.go` (`ComputeIPAssigned`,
  `ComputeIPReleased`). The allocate/attach paths emit `assigned`
  with `trigger="allocate" | "attach"`; the detach/release paths
  emit `released` with `trigger="detach" | "release"`. Plugins
  subscribed to `compute.ip.*` get the full lifecycle.
- **RBAC:** 3 new permission slugs (`compute.ip_pool.manage` —
  platform-admin only; `compute.floating_ip.manage` —
  tenant.admin + tenant.member; `compute.floating_ip.read` —
  tenant.viewer + above). The pool admin tree is platform-admin
  only because pools are operator-owned resources; tenant admins
  cannot register or modify pools.
- **Audit:** 10 new audit action slugs covering pool/range CRUD +
  floating IP allocate/attach/detach/release/set_ptr; 3 new
  resource types (`ip_pool`, `ip_pool_range`, `floating_ip`). The
  pool admin actions carry `actor_user_id` only (no `tenant_id` —
  global action); the floating-IP actions carry both.
- **OpenAPI drift:** the `openapi-verify` target passes; the
  regenerated Go server types + Go client SDK + TS schema are all
  committed.
- **Provider test boundary:** per ADR-0027 the test boundary stays
  at the operations-interface level. The existing in-memory fake
  in `compute/service_integration_test.go` was extended with
  `CreateNetworkForward` / `DeleteNetworkForward` so it satisfies
  the new `incusForwardOps` sub-interface.
- **Out of scope, resolved:**
  1. **UI:** deferred to a follow-up WS. The backend ships the
     full control-plane surface; the dashboard can consume it
     as-is.
  2. **BGP / FRR / NAT enforcement:** operator concern. Documented
     in `deployments/README.md`; Lahijan does not manage the data
     plane.
  3. **Per-IP rate limiting + abuse handling:** deferred to a
     follow-up WS. Needs host-level enforcement.
  4. **Egress metering:** deferred to a follow-up WS. Needs
     data-plane integration (NetFlow / IPFIX).
  5. **Reconcile worker:** deferred to a follow-up WS. Not needed
     yet — the operator is the only one who can hand-edit Incus,
     and they are also the one who decided to do so.

### Unticked DoD boxes

One: **relevant UI page done** — deferred. The backend ships the full
control-plane surface; the dashboard work is its own follow-up WS
(WS-30a) because it needs forms, validation, and i18n in two locales.

### Suggested follow-up WSs

1. **WS-30a — Dashboard floating-IP panel.** Add the IP-pool admin
   view + the per-tenant floating-IP list + the attach/detach buttons
   to the dashboard (web/). Wires the 10 new endpoints into the
   TanStack Query hooks; the permission gates reuse the existing
   `usePerm` hook.
2. **WS-30b — Per-IP-hour metering worker.** A River worker that
   emits one usage row per allocated floating IP per minute, priced
   via the catalog. The compute service's `meter` seam is already
   in place; the worker just plugs into it.
3. **WS-30c — Reconcile worker.** A periodic River worker that
   re-pushes the Postgres-recorded forward to Incus on every tick.
   Closes the drift window when the operator hand-edits the daemon.
4. **WS-30d — Per-IP rate limiting + abuse handling.** Host-level
   enforcement via nftables; the audit surface for abuse events.
5. **WS-30e — Egress metering.** NetFlow / IPFIX ingestion from the
   host; per-GB pricing.

## Notes

- This WS is operationally heavy when the operator wants the full
  surface (BGP, RIR allocation, abuse handling). Most self-hosters
  won't need it; it's primarily for SaaS-style deployments.
- Floating IP semantics: detach → instance keeps running but loses
  the IP; attach to another instance → traffic switches.
- Per pillar 1 the user-facing copy is "IP pool" + "floating IP".
  Incus, BGP, FRR are never named in the API or UI.
