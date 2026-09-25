---
title: Observability
description: Read Lahijan logs, use the health endpoints, reach Grafana and Jaeger, and decide what to alert on.
---

This page covers the signals a production Lahijan stack gives you today: container logs, health checks and probe endpoints, and the observability services in the compose file. It also says plainly which parts of the telemetry pipeline are not wired up yet, so you do not spend time looking for data that is not there.

## Logs

The Lahijan process writes its logs to standard output and standard error. They come from three places:

- Application messages through Go's `log/slog` default handler and GoFiber's logger, as text lines with a level (such as `INFO`, `WARN` or `ERROR`) and key/value details. The output is plain text, not JSON.
- Fatal startup errors through Go's `log` package, followed by the process exiting.
- One line per HTTP request from GoFiber's request logger, when `http.server.logger.enabled` is true (the default).

Read them with Docker:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs -f lahijan
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs --since 1h lahijan | grep -E "WARN|ERROR"
```

Every service uses the `json-file` log driver, rotated at 10 MB with three files kept, so older lines are lost. Ship container logs to a central store with your own log shipper if you need longer retention.

Set `LAHIJAN_DEBUG=true` (in a compose override) for more verbose GoFiber logging while you investigate a problem, and turn it off again afterwards.

> [!WARNING]
> There is no log redaction handler in this release. On first boot the generated admin password is written to the log at WARN level, and it stays in the container log files until they rotate. Change that password right after the first sign-in.

## Health checks

### Probe endpoints

With `http.server.healthcheck.enabled` (on by default and in production), Lahijan answers two probe paths:

| Key                                         | Default path             |
| ------------------------------------------- | ------------------------ |
| `http.server.healthcheck.livenessEndpoint`  | `/healthcheck/liveness`  |
| `http.server.healthcheck.readinessEndpoint` | `/healthcheck/readiness` |

Both return `200` whenever the HTTP server is running. They do not check PostgreSQL, Incus, PowerDNS or SeaweedFS. Caddy forwards `/healthcheck/*`, so you can probe from outside:

```sh
curl -fsS https://app.example.com/healthcheck/liveness
```

An external probe of this URL checks DNS, the TLS certificate, Caddy and the Lahijan process in one request.

`GET /health` returns `{"status":"ok","version":"..."}`. Caddy does not forward it, so call it from inside the container (see [Upgrades and migrations](/docs/operations/upgrades#check-the-running-version)).

### Container health

Every long-running service in the compose file has a health check, for example `pg_isready` for PostgreSQL, `pdns_control rping` for PowerDNS, `incus list` for Incus and the liveness path for Lahijan. See them with:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps
docker inspect --format '{{json .State.Health}}' lahijan-prod-app
```

## Telemetry services in the stack

The compose file includes an OpenTelemetry collector and four backends:

| Service          | Role                                                                                                           | How to reach it                          |
| ---------------- | -------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| `otel-collector` | Receives OTLP on 4317 (gRPC) and 4318 (HTTP); sends traces to Jaeger, logs to Loki and exposes metrics on 8889 | Internal only                            |
| `jaeger`         | Trace storage (in memory, up to 50000 traces) and UI                                                           | `https://app.example.com/jaeger/`        |
| `loki`           | Log storage                                                                                                    | Internal only (`http://loki:3100`)       |
| `prometheus`     | Metrics, 15 day retention                                                                                      | Internal only (`http://prometheus:9090`) |
| `grafana`        | Dashboards                                                                                                     | `https://app.example.com/grafana/`       |

Caddy protects `/grafana` and `/jaeger` with basic auth (`GRAFANA_BASIC_AUTH_USER`/`GRAFANA_BASIC_AUTH_HASH` and `JAEGER_BASIC_AUTH_USER`/`JAEGER_BASIC_AUTH_HASH`). The fallback is `admin` with the password `admin`, so set real hashes:

```sh
docker run --rm -it caddy:2.8-alpine caddy hash-password
```

Put the hashes in `.env.prod` in single quotes and recreate Caddy. Grafana then asks for its own login (`GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD`); sign-up is disabled. Comments in the compose file mention a `LAHIJAN_OBSERVABILITY_PUBLIC` switch, but nothing reads it; to remove these routes, edit the Caddyfile.

### Current limits

Be aware of these gaps in the shipped release:

- The Lahijan binary creates trace spans and job metrics through the OpenTelemetry API, but it does not install an OpenTelemetry SDK or OTLP exporter. The `OTEL_EXPORTER_OTLP_*` variables on the `lahijan` service are not used, so no traces, metrics or logs from Lahijan reach the collector.
- Lahijan has no `/metrics` endpoint. The collector's Prometheus receiver targets `host.docker.internal:3000`, which only exists in the development setup.
- `deployments/prometheus/prometheus.yml` points Prometheus at `otel-colector:8889`, but the service is named `otel-collector`, so that scrape target shows as down.
- Grafana starts without data sources. Add them by hand under **Connections > Data sources** using `http://prometheus:9090`, `http://loki:3100` and `http://jaeger:16686`.

Until this is wired up, container logs and health checks are your main signals.

## What to alert on

These checks only use what the stack provides:

| Check                | How                                                                                                                                                                                                                                                          |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Dashboard and API up | External HTTPS probe of `/healthcheck/liveness`.                                                                                                                                                                                                             |
| Container health     | Any service not `healthy`, or restarting repeatedly, in `docker compose ps`.                                                                                                                                                                                 |
| Certificate expiry   | Days left on the certificate served for the dashboard host (and the S3 host if you have one).                                                                                                                                                                |
| Authoritative DNS    | `dig @<host-ip> <a zone you host> SOA` from outside answers.                                                                                                                                                                                                 |
| S3 endpoint          | The public S3 URL answers from outside.                                                                                                                                                                                                                      |
| Disk space           | Docker's volume directory and `/var/lib/incus` on the host.                                                                                                                                                                                                  |
| Backups              | Exit status and output of `scripts/backup.sh`, and the size of the newest archive.                                                                                                                                                                           |
| Lahijan warnings     | Log lines such as `incus provider ping failed at startup`, `powerdns provider ping failed at startup`, `seaweedfs provider ping failed at startup`, `seaweedfs IAM sync failed at startup`, `seaweedfs bucket CORS backfill failed` and `bootstrap: failed`. |
| Failed jobs          | The River UI at `/admin/jobs/ui`; see [Background jobs](/docs/admin/jobs).                                                                                                                                                                                   |

For security events (privilege changes, sign-in failures, deletions), use the audit log; see [Audit log](/docs/audit/overview).
