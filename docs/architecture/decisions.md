# Architecture decisions (summary)

> Full text per decision: see [`../adr/`](../adr/).

## At-a-glance

| # | Decision | Why |
|---|----------|-----|
| 0001 | Module path `github.com/avestura/lahijan` | Zero DNS setup; matches GitHub URL. |
| 0002 | Multi-tenant row-level isolation | One DB, cheap ops, SaaS+self-host same code path. |
| 0003 | sqlc + golang-migrate + pgx | SQL-first; type-safe codegen; idiomatic Go. |
| 0004 | All auth methods (pw + OAuth + OIDC + SAML + MFA) | One deployer can serve homelab → enterprise. |
| 0005 | Single-host now, multi-host-ready | One-command install today; no rewrite later. |
| 0006 | Managed deps (Lahijan ships the stack) | One-command install; reproducible dev = prod. |
| 0007 | Shared Postgres, separate logical DBs | One container, three DBs, three users. |
| 0008 | River for jobs | PostgreSQL-native; no new deps; durable. |
| 0009 | Multi-node-ready from day 1 | Small abstraction discipline now, no rewrite later. |
| 0010 | Full Incus surface | Power-user friendly; tenant = Incus project. |
| 0011 | Direct S3 + Lahijan control plane | No proxy bottleneck; standard S3 tools work. |
| 0012 | WASM plugin system in MVP | Extensibility is a first-class feature, not bolt-on. |
| 0013 | Ledger-only billing | Works in every country; no PCI scope. |
| 0014 | Vite SPA for both apps | One toolchain; shared design system. |
| 0015 | REST + OpenAPI as source of truth | Types can't drift; clients are generated. |
| 0016 | Full OpenTelemetry from day 1 | Logs + metrics + traces; every request traceable. |
| 0017 | Full i18n (en + fa, RTL) from day 1 | Brand is Iranian; pipeline tested from day 1. |
