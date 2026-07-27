# Architecture Decision Records

Each ADR captures **one** architectural decision: the context, the options
considered, the choice, and its consequences. ADRs are immutable once
Accepted; corrections land as new ADRs that supersede prior ones.

## Index

| # | Title | Status | Date |
|---|-------|--------|------|
| [0001](./0001-module-path.md) | Module path: `github.com/avestura/lahijan` | Accepted | 2026-07-17 |
| [0002](./0002-tenancy-model.md) | Multi-tenant, row-level isolation | Accepted | 2026-07-17 |
| [0003](./0003-db-tooling.md) | sqlc + golang-migrate on PostgreSQL | Accepted | 2026-07-17 |
| [0004](./0004-auth-methods.md) | All auth methods (password + OAuth + OIDC + SAML + MFA) | Accepted | 2026-07-17 |
| [0005](./0005-mvp-topology.md) | Single-host now, multi-host-ready | Accepted | 2026-07-17 |
| [0006](./0006-managed-dependencies.md) | Lahijan ships the whole stack | Accepted | 2026-07-17 |
| [0007](./0007-shared-postgres.md) | Shared Postgres, separate logical DBs | Accepted | 2026-07-17 |
| [0008](./0008-river-job-queue.md) | River (PostgreSQL-native) for jobs | Accepted | 2026-07-17 |
| [0009](./0009-multi-node-ready.md) | Design for multi-node from day 1 | Accepted | 2026-07-17 |
| [0010](./0010-full-incus-surface.md) | Expose the full Incus API surface | Accepted | 2026-07-17 |
| [0011](./0011-direct-s3-access.md) | Direct S3 for data + Lahijan for control plane | Accepted | 2026-07-17 |
| [0012](./0012-wasm-full-mvp.md) | WASM plugin system in MVP scope | Accepted | 2026-07-17 |
| [0013](./0013-ledger-billing.md) | Ledger-only billing for MVP (admin top-up) | Accepted | 2026-07-17 |
| [0014](./0014-frontend-vite-spa.md) | Vite SPA for both marketing + dashboard | Accepted | 2026-07-17 |
| [0015](./0015-rest-openapi.md) | REST + OpenAPI as source of truth | Accepted | 2026-07-17 |
| [0016](./0016-full-otel.md) | Full OpenTelemetry from day 1 | Accepted | 2026-07-17 |
| [0017](./0017-i18n-from-day-one.md) | Full i18n from day 1 (en + fa, RTL) | Accepted | 2026-07-17 |
| [0018](./0018-ws-06-auth-dependencies.md) | WS-06 auth dependency choices | Accepted | 2026-07-17 |
| [0019](./0019-append-only-audit-outcomes.md) | Append-only audit outcome trail (MarkOutcome pattern) | Accepted | 2026-07-18 |
| [0020](./0020-saml-library-choice.md) | SAML library choice — `crewjam/saml` | Accepted | 2026-07-18 |
| [0021](./0021-mfa-dependencies.md) | MFA library choices — `go-webauthn/webauthn` + `pquerna/otp` | Accepted | 2026-07-18 |
| [0022](./0022-river-migrations-bundled.md) | River migrations bundled into golang-migrate (split at enum boundary) | Accepted | 2026-07-18 |
| [0023](./0023-wasm-target-wasi-preview2.md) | WASM plugin target — plain `wasm32-unknown-unknown` (no WASI) for MVP | Accepted | 2026-07-18 |
| [0024](./0024-wasm-host-function-abi.md) | WASM Host Function ABI — flat i32 pointer-passing | Accepted | 2026-07-18 |
| [0025](./0025-incus-client-library-choice.md) | Incus client library — thin internal REST client over the Incus REST API | Accepted | 2026-07-18 |
| [0026](./0026-powerdns-client-library-choice.md) | PowerDNS client library — thin internal REST client over the PDNS HTTP API | Accepted | 2026-07-18 |
| [0027](./0027-seaweedfs-client-library-choice.md) | SeaweedFS client library — AWS SDK v2 (S3) + thin HTTP (Filer/IAM), interface-injected for tests | Accepted | 2026-07-18 |
| [0028](./0028-website-prerendering-stack.md) | Marketing website prerendering stack — react-router-dom + vite-react-ssg (deviation from TanStack Router, scoped to `website/`) | Accepted | 2026-07-19 |
| [0029](./0029-test-sandbox-topology.md) | Test sandbox topology — fakes per provider, coverage gate at the unit-test baseline | Accepted | 2026-07-20 |
| [0030](./0030-production-deployment-topology.md) | Production deployment topology — Caddy, single-host, four-script ops | Accepted | 2026-07-20 |
| [0031](./0031-novnc-web-console-proxy.md) | noVNC web console proxy — vendor assets under web/public/novnc/, in-process WebSocket bridge | Accepted | 2026-07-20 |
| [0032](./0032-scheduled-snapshots-backups-design.md) | Scheduled snapshots + off-host backup design — River-driven schedules, BackupTarget interface, AES-GCM credential envelope | Accepted | 2026-07-21 |
| [0033](./0033-multi-node-cluster-design.md) | Multi-node cluster design — PlacementDriver + ClusterPlacementDriver, Incus cluster API, advisory-locked scheduling | Accepted | 2026-07-21 |
| [0034](./0034-payment-gateway-stripe-design.md) | Payment Gateway (Stripe) design — thin HTTP client, ledger-layered, webhook-first | Accepted | 2026-07-21 |
| [0035](./0035-dns-recursor-dnsdist-registrar-design.md) | DNS Recursor + dnsdist + Registrar Resale design — PowerDNS recursor + dnsdist in front + thin internal RegistrarProvider | Accepted | 2026-07-21 |
| [0036](./0036-s3-lifecycle-versioning-object-lock-design.md) | S3 Lifecycle / Versioning / Object Lock design — Postgres source-of-truth, SDK push + River evaluator, bucket-level policy (WS-29) | Accepted | 2026-07-21 |
| [0037](./0037-public-ip-floating-ips-design.md) | Public IP Assignment & Floating IPs design | Accepted | 2026-07-21 |
| [0038](./0038-plugin-sdk-go-design.md) | Plugin SDK (Go) design — sdk-go/ module, inline response-buffer ABI, typed errors | Accepted | 2026-07-25 |
| [0039](./0039-wasi-runtime-mode.md) | WASI plugin runtime mode — filtered importer, dedicated WASI slugs, `*` escape hatch | Accepted | 2026-07-25 |
| [0040](./0040-incus-in-container-topology.md) | Incus-in-container topology — privileged `incus` service via `ghcr.io/cmspam/incus-docker` (supersedes the Incus-placement dimension of ADR-0005) | Accepted | 2026-07-26 |

## How to write a new ADR

1. Copy [`0000-template.md`](./0000-template.md) to `NNNN-short-title.md`
   where `NNNN` is the next free number.
2. Fill every section. Don't write a "Conclusion" without a "Context."
3. Status starts at `Proposed`. Flip to `Accepted` once a maintainer approves.
4. Add a row to the index above.
5. Commit: `docs(adr): NNNN <short title>`.

## Superseding

To reverse or replace an earlier ADR:

1. Create a new ADR that says `Supersedes ADR-NNNN`.
2. Update the old ADR: change Status to `Superseded by ADR-MMMM`.
3. Update both rows in the index.
4. Commit: `docs(adr): MMMM supersedes NNNN`.

ADRs are never deleted; the trail matters.
