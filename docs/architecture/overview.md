# Architecture Overview

This document is the high-level map. For rules, see
[`conventions.md`](./conventions.md). For decisions, see
[`../adr/`](../adr/).

## One-paragraph summary

Lahijan is a monolithic Go backend plus a React+TypeScript SPA frontend, deployed
as a single docker-compose stack. It presents a unified self-service cloud
portal to end users, hiding three best-of-breed backends behind a clean REST
API: Incus for compute (containers + VMs), PowerDNS Authoritative for DNS, and
SeaweedFS for S3-compatible object storage.

## Component diagram (text)

```
                    ┌─────────────────────────────────────────────┐
                    │              End user (browser)              │
                    └───────────┬──────────────────┬──────────────┘
                                │                  │
                       REST/JSON │                  │ S3 (direct, with
                          /api/v1│                  │  Lahijan-minted creds)
                                ▼                  ▼
   ┌────────────────────────────────────────┐  ┌──────────────────────┐
   │ Lahijan backend (GoFiber, monolith)    │  │ SeaweedFS S3         │
   │  ├─ api/        (HTTP handlers)        │  │  (data plane)        │
   │  ├─ domain/     (services)             │  └──────────┬───────────┘
   │  ├─ database/   (sqlc → pgx → ─────────┼──────────┐  │
   │  │               Postgres)             │          │  │
   │  ├─ providers/                         │          │  │
   │  │   ├─ incus/      ───── Incus REST───┼───┐      │  │
   │  │   ├─ powerdns/   ───── PDNS HTTP ───┼─┐ │      │  │
   │  │   └─ seaweedfs/ ───── SeaweedFS ────┼─┼─┼──────┘  │
   │  ├─ jobs/      (River → Postgres)      │ │ │         │
   │  ├─ wasm/      (wazero sandbox)        │ │ │         │
   │  └─ observability/ (slog + OTel)       │ │ │         │
   └────────────────────────┬───────────────┘ │ │         │
                            │                 │ │         │
                  OTLP logs/metrics/traces    │ │         │
                            ▼                 │ │         │
                  ┌─────────────────┐         │ │         │
                  │  OTel collector │         │ │         │
                  └────────┬────────┘         │ │         │
                  ┌────────┴────────┐         │ │         │
                  │ Jaeger / Loki / │         │ │         │
                  │   Prometheus    │         │ │         │
                  └─────────────────┘         │ │         │
                                              ▼ ▼         │
                                       ┌───────────────┐  │
                                       │  PostgreSQL   │◀─┘
                                       │  lahijan_db   │
                                       │  pdns_db      │
                                       │  seaweed_db   │
                                       └───────────────┘
                                              ▲
                                              │ gpgsql / filer
                                              │
                              ┌───────────────┴───────────────┐
                              │                               │
                       ┌──────▼────────┐              ┌───────▼───────┐
                       │ PowerDNS Auth │              │  SeaweedFS    │
                       │   (DNS zone   │              │   Filer +     │
                       │    serving)   │              │   Master +    │
                       └───────────────┘              │   Volume      │
                                      └──────────────┴───────────────┘

                       ┌───────────────────────────────┐
                       │  Incus daemon (host, kernel)  │◀── Lahijan talks to it
                       │   - tenant-A project          │     via Unix socket
                       │   - tenant-B project          │
                       └───────────────────────────────┘
```

## Logical layers

| Layer | Folder | Responsibility |
|-------|--------|----------------|
| Entrypoint | `cmd/lahijan/` | Parse flags; call `program.Start()`; exit. |
| Bootstrap | `internal/app/lahijan/program/` | Build Fiber app, wire middleware, register routes, listen. |
| Config | `internal/app/lahijan/conf/` | Viper + pflag + computed defaults. |
| HTTP API | `internal/app/lahijan/api/` | Handlers, routers, middleware, error envelope, OpenAPI. |
| Domain | `internal/app/lahijan/domain/<area>/` | Business logic per area. |
| Persistence | `internal/app/lahijan/database/` | sqlc queries + generated code + repository wrappers. |
| Providers | `internal/app/lahijan/providers/<incus\|powerdns\|seaweedfs>/` | Backend drivers. |
| Jobs | `internal/app/lahijan/jobs/` | River client + worker supervisor + job registry. |
| WASM | `internal/app/lahijan/wasm/` | wazero runtime, permission enforcer, host functions. |
| Observability | `internal/app/lahijan/observability/` | slog handlers, OTel SDK, redact middleware. |
| Auth | `internal/app/lahijan/auth/` | Password, OAuth, OIDC, SAML, MFA, RBAC, audit. |
| i18n | `internal/app/lahijan/i18n/` | go-i18n bundles (en, fa). |
| Frontend (dashboard) | `web/` | React + TS SPA. |
| Frontend (marketing) | `website/` | React + TS SPA. |
| Docs | `docs/` + `docs-site/` | Markdown content + Docusaurus renderer. |
| Deploy | `deployments/` | docker-compose dev / prod / test. |

## Tenancy flow

1. User logs in → middleware sets `user_id` in `c.Locals`.
2. User's session includes a `tenant_id` (current active tenant; user may
   belong to several).
3. `TenantMiddleware` sets `tenant_id` in `c.Locals` and in the request
   `context.Context`.
4. Service layer passes that context down to repositories.
5. Repository layer extracts `tenant_id` and bakes it into every sqlc query.
6. Cross-tenant access is structurally impossible without bypassing the
   repository layer (which lint + review forbid).

## Compute flow (example: user starts an instance)

1. User POSTs to `/api/v1/compute/instances` with the request body.
2. Router → `RequirePerm("compute.instance.create")` → handler.
3. Handler validates via zod-equivalent (Go validator), calls
   `domain/compute.Service.CreateInstance(ctx, tenantID, params)`.
4. Service:
   a. Checks the tenant's quota.
   b. Checks the user's balance (via `domain/billing.Service`).
   c. Emits an audit event (`status: pending`).
   d. Calls `providers/incus.Client.CreateInstance` in the tenant's project.
   e. Persists the instance row via `database/compute_repo`.
   f. Schedules a metering job (River).
   g. Updates the audit event (`status: success`).
5. Handler returns `201 Created` with the instance representation.

## Multi-node readiness

The compute flow above doesn't change if Lahijan runs as three replicas against
one Postgres:

- The Incus call goes through a `PlacementDriver` interface; the MVP
  implementation routes to the local socket. WS-26 swaps in a
  `ClusterPlacementDriver` that picks a cluster member.
- The metering job is a River job; whichever replica wins the job runs it.
- The audit row is committed atomically with the instance row.

See ADR-0009.

## What's deferred

Phase 7 workstreams (`docs/workstreams/WS-24..30`) document the deferred
features: noVNC, scheduled snapshots, multi-node cluster, payment gateway,
DNS recursor, S3 lifecycle, public IPs.
