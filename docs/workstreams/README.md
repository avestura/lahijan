# Workstream Index

Lahijan is built workstream-by-workstream. Each WS is a self-contained brief
in `WS-XX-<name>.md`. Phases denote dependency ordering, not strict
serialization.

The ws-implementer subagent (`.opencode/agent/ws-implementer.md`) and the
`adding-a-workstream` skill enforce the workflow.

## How to use this index

1. Pick a WS that is `pending` and whose `Depends on` are all `done`.
2. Open the WS doc; read it end to end.
3. Load its **"Required reading"** list.
4. Branch `feat/ws-XX-<short>` and implement.
5. Update the `Status` field here and in the WS doc when you start/finish.

## Status legend

| Status | Meaning |
|--------|---------|
| `pending` | Doc accepted; not started. |
| `in-progress` | Branch exists; PRs flowing. |
| `done` | Merged to main; DoD met. |
| `deferred` | Doc exists; intentionally not in current phase (Phase 7). |
| `blocked` | Cannot proceed; unblocker noted in doc. |
| `cancelled` | Decided not to do; doc kept for the trail. |

## Phase 0 — Foundations

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-01](./WS-01-repo-tooling-hygiene.md) | Repo & Tooling Hygiene | done | — |
| [WS-02](./WS-02-ai-context-layer.md) | AI Context Layer & Docs Framework | in-progress | WS-01 |
| [WS-03](./WS-03-database-persistence-core.md) | Database & Persistence Core | pending | WS-02 |
| [WS-04](./WS-04-config-observability-foundation.md) | Config Expansion + Observability Foundation | pending | WS-03 |
| [WS-05](./WS-05-rest-openapi-pipeline.md) | REST API Framework + OpenAPI Pipeline | done | WS-04 |

## Phase 1 — Security & Identity

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-06](./WS-06-core-auth.md) | Core Auth (password + tokens + sessions) | done | WS-05 |
| [WS-07a](./WS-07a-external-idps-oauth-oidc.md) | External IdPs: OAuth + OIDC | done | WS-06 |
| [WS-07b](./WS-07b-external-idps-saml.md) | External IdPs: SAML 2.0 | done | WS-06 |
| [WS-07c](./WS-07c-mfa.md) | Multi-Factor Auth | done | WS-06 |
| [WS-08](./WS-08-rbac-audit-log.md) | RBAC + Audit Log | done | WS-06 |

## Phase 2 — Platform Services

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-09](./WS-09-job-system.md) | Job System (River) | done | WS-03 |
| [WS-10a](./WS-10a-wasm-runtime-permissions.md) | WASM Runtime + Permission System | done | WS-05, WS-08 |
| [WS-10b](./WS-10b-wasm-host-functions-event-bus.md) | WASM Host Functions + Event Bus | done | WS-10a |
| [WS-10c](./WS-10c-wasm-sample-plugins-marketplace.md) | WASM Sample Plugins + Marketplace Scaffolding | done | WS-10b |

## Phase 3 — Infrastructure Providers

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-11](./WS-11-incus-provider.md) | Incus Provider | pending | WS-05 |
| [WS-12](./WS-12-powerdns-provider.md) | PowerDNS Provider | pending | WS-05 |
| [WS-13](./WS-13-seaweedfs-provider.md) | SeaweedFS Provider | pending | WS-05 |

## Phase 4 — User-Facing Modules

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-14](./WS-14-compute-module.md) | Compute Module | pending | WS-11, WS-08, WS-09 |
| [WS-15](./WS-15-dns-module.md) | DNS Module | pending | WS-12, WS-08 |
| [WS-16](./WS-16-storage-module.md) | Object Storage Module | pending | WS-13, WS-08 |
| [WS-17](./WS-17-billing-metering.md) | Billing & Metering | pending | WS-08, WS-09 |

## Phase 5 — Frontend

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-18](./WS-18-frontend-foundation.md) | Frontend Foundation | pending | WS-05 |
| [WS-19](./WS-19-marketing-website.md) | Marketing Website | pending | WS-18 |
| [WS-20](./WS-20-dashboard-shell-compute-ui.md) | Dashboard Shell + Compute UI | pending | WS-18, WS-14 |
| [WS-21](./WS-21-module-uis.md) | DNS/S3/Billing/Plugins/Audit UIs | pending | WS-18, WS-15, WS-16, WS-17 |

## Phase 6 — Quality & Release

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-22](./WS-22-sandbox-integration-test-harness.md) | Sandbox / Integration Test Harness | pending | WS-14, WS-15, WS-16 |
| [WS-23](./WS-23-production-deployment-ops.md) | Production Deployment & Ops | pending | WS-22 |

## Phase 7 — Deferred Backlog

Docs ready. Work not in MVP. Will be scheduled when MVP is shipped.

| WS | Title | Status | Depends on |
|----|-------|--------|------------|
| [WS-24](./WS-24-novnc-web-console.md) | noVNC Web Console for VMs | deferred | WS-11, WS-20 |
| [WS-25](./WS-25-scheduled-snapshots-backups.md) | Scheduled Snapshots & Backups | deferred | WS-11, WS-09 |
| [WS-26](./WS-26-multi-node-cluster.md) | Multi-Node / HA Control Plane | deferred | WS-23 |
| [WS-27](./WS-27-payment-gateway-stripe.md) | Payment Gateway (Stripe) + Subscriptions | deferred | WS-17 |
| [WS-28](./WS-28-dns-recursor-dnsdist-registrar.md) | DNS Recursor + dnsdist + Registrar Resale | deferred | WS-12 |
| [WS-29](./WS-29-s3-lifecycle-versioning.md) | S3 Lifecycle / Versioning / Object Lock | deferred | WS-13 |
| [WS-30](./WS-30-public-ip-floating-ips.md) | Public IP Assignment & Floating IPs | deferred | WS-11, WS-26 |

## Dependency graph (text)

```
Phase 0 (Foundations):
  WS-01 ─┬─ WS-02 ─┬─ WS-03 ─┬─ WS-04 ── WS-05
         │          │          │
         │          │          └────────── WS-09 (River)
         │          │
         │          └─ (all docs depend on WS-02 for the template)
         │
         └─ (CI + Makefile from WS-01 used everywhere)

Phase 1 (Security):   WS-06 ─┬─ WS-07a, WS-07b, WS-07c
                              └─ WS-08

Phase 2 (Platform):   WS-09 ── (jobs used by WS-17, WS-25)
                      WS-10a ── WS-10b ── WS-10c

Phase 3 (Providers):  WS-11, WS-12, WS-13 (independent of each other)

Phase 4 (Modules):    WS-14 (needs WS-11+WS-08+WS-09)
                      WS-15 (needs WS-12+WS-08)
                      WS-16 (needs WS-13+WS-08)
                      WS-17 (needs WS-08+WS-09)

Phase 5 (Frontend):   WS-18 ─┬─ WS-19
                               ├─ WS-20 (needs WS-14)
                               └─ WS-21 (needs WS-15, WS-16, WS-17)

Phase 6 (Quality):    WS-22 (needs WS-14, WS-15, WS-16) ── WS-23

Phase 7 (Deferred):   WS-24..30 (varies; see each doc)
```
