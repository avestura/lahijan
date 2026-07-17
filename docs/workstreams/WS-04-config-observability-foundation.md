# WS-04 · Config Expansion + Observability Foundation

```
Status: pending
Phase: 0
Depends on: WS-03
Unblocks: WS-05 (API), every WS that uses config or logs
```

## Goal

Two concerns in one WS (they're tightly coupled via Fiber middleware): (a)
extend the existing Viper config system to cover every provider + cross-cutting
subsystem we'll need, and (b) wire up structured logging and full
OpenTelemetry (logs + metrics + traces) from the start. Every subsequent WS
should be able to `slog.Info(...)` and have it land in the collector.

## Scope

**In scope:**

### Config expansion (`conf/`)
Extend `.lahijan.conf.default.yaml` with new sections and pflag auto-gen:

- `database.*` (DSN, max conns, statement timeout)
- `providers.incus.*` (socket path or remote URL, project prefix)
- `providers.powerdns.*` (API URL, API key from env, default TTL)
- `providers.seaweedfs.*` (S3 endpoint, admin creds from env, region)
- `otel.*` (exporter endpoint, service name, sample ratio, resource attrs)
- `smtp.*` (host, port, user, pass from env, from address)
- `i18n.*` (default locale, supported locales)
- `auth.*` (session TTL, refresh TTL, password min entropy, OAuth/OIDC/SAML
  toggles)
- `billing.*` (zero-balance grace period, min top-up)
- `wasm.*` (max memory per plugin, exec timeout)

### Observability (`observability/`)
- `log/slog` handler with JSON formatter, log level from `conf`.
- Redact middleware: scrub common sensitive field names (`password`, `token`,
  `authorization`, `*_secret`, `*_key`).
- OTel SDK setup: tracer provider, meter provider, logger provider.
- OTLP exporter (gRPC + HTTP) pointing at the collector's URL from `conf`.
- GoFiber auto-instrumentation (request spans).
- pgx auto-instrumentation (DB spans).
- Outgoing HTTP auto-instrumentation (provider calls).
- `/metrics` endpoint (Prometheus format, scraped by collector).
- Trace context propagation through River jobs.

### Compose
- Add `otel-collector`, `jaeger`, `loki`, `prometheus` to
  `docker-compose.dev.yml`.
- Add `otel/otel-collector-config.yaml` to `deployments/`.

**Out of scope:**
- The auth subsystem itself (WS-06).
- Provider clients themselves (WS-11..13).
- Billing logic (WS-17).

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0016-full-otel.md`
- `docs/architecture/conventions.md#observability`
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- Expanded `.lahijan.conf.default.yaml` + matching `conf/` getters.
- `observability/logging.go` (slog setup + redact).
- `observability/otel.go` (SDK + exporters + shutdown).
- `observability/metrics.go` (metrics handler at `/metrics`).
- Middleware wired in `program/`: OTel trace middleware + slog request logger.
- OTel stack in `docker-compose.dev.yml`; collector config committed.
- Docs: `docs/architecture/observability.md` (operator view of the dashboards).

## Definition of Done

- [ ] every key in `.lahijan.conf.default.yaml` has a `Get*` helper in `conf/`
- [ ] every config key is overridable via `LAHIJAN_*` env + CLI pflag
- [ ] `make dev-up` brings up the OTel stack
- [ ] a request to `/health` produces a trace in Jaeger
- [ ] logs land in Loki with `trace_id` field
- [ ] `/metrics` returns Prometheus-format metrics
- [ ] redact middleware demonstrably scrubs a `password` field in tests
- [ ] trace context survives into River jobs (integration test)
- [ ] `make lint test` green

## Open questions

- Sample ratio default: 1.0 in dev, 0.1 in prod? (Default: yes.)
- Should we ship Grafana too, or just Jaeger + Loki + Prometheus UIs?
  (Default: ship Grafana with provisioned dashboards; one pane of glass.)

## Notes

- The OTel collector config should support sending to *all three* backends
  simultaneously (traces → Jaeger, logs → Loki, metrics → Prometheus).
- pgx v5 has built-in OTel support via `pgxotel`.
