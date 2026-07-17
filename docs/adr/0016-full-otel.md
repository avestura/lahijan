# ADR-0016: Full OpenTelemetry from day 1

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

A cloud platform like Lahijan touches multiple external systems, runs async
jobs, and serves many concurrent tenants. Observability is not optional.

Options considered:

- **Structured logs only (slog)** — minimum viable; hard to correlate across
  services; no distributed tracing.
- **+ Prometheus metrics** — adds SLO-grade metrics; still no traces.
- **Full OpenTelemetry (logs + metrics + traces)** — maximum observability;
  adds OTel SDK + a collector to the stack.

## Decision

Lahijan ships **full OpenTelemetry** from day 1:

- **Logs**: `log/slog` with JSON handler, bridged into OTel via the log
  signal. Every log line carries `trace_id` + `span_id` when in a trace.
- **Metrics**: OTel metrics SDK instruments HTTP handlers, DB queries,
  provider calls, job execution, billing.
- **Traces**: OTel trace SDK auto-instruments GoFiber, pgx, outgoing HTTP.
  Spans propagate via W3C Trace Context.
- **Export**: OTLP exporter to a collector running in the compose stack
  (ADR-0006). The collector fans out to Jaeger (traces), Loki (logs),
  Prometheus (metrics).

A redact handler strips secrets, tokens, and PII before export.

## Consequences

- **Positive:** every request is traceable from the dashboard through the API,
  to the DB, to Incus/PDNS/SeaweedFS.
- **Positive:** SLOs can be defined on metrics out of the box.
- **Positive:** logs correlate to traces via trace_id.
- **Negative:** small CPU + memory cost per request; acceptable for the value.
- **Negative:** operator gets three more containers (collector + Jaeger + Loki
  + Prometheus); mitigated by shipping them pre-configured.

## Compliance

- `internal/app/lahijan/observability/` contains the SDK setup and the redact
  handler.
- The compose stack includes `otel-collector`, `jaeger`, `loki`, `prometheus`.
- Every service in the stack exports OTLP to the collector.
- Trace context propagates through River jobs and plugin host calls.

## References

- [OpenTelemetry for Go](https://opentelemetry.io/docs/languages/go/)
- [OTLP collector](https://opentelemetry.io/docs/collector/)
- ADR-0006 (managed deps — collector in stack)
- WS-04 (config + observability foundation)
