---
title: Architecture
description: How the Lahijan binary, its backends and the production compose services fit together, and which parts hold state.
---

This page explains what runs where in a production Lahijan install: the single Go process, the backend services it drives, how traffic moves between them and which components keep data you must protect. Read it before you change the compose file or plan backups.

## The Lahijan process

Lahijan is one Go binary (`cmd/lahijan`, started by `program.Start`). The production image `ghcr.io/avestura/lahijan:<tag>` runs it as `/etc/lahijan/server`. One process contains:

| Part           | What it does                                                                                                                                                                                                       |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| REST API       | GoFiber server for every route under `/api/v1/`, plus `/health` and the `/healthcheck/*` probes. Listens on port 8080 in the production stack.                                                                     |
| Auth           | Passwords (argon2id), sessions, personal access tokens, OAuth, OIDC, SAML 2.0, TOTP, WebAuthn and email links.                                                                                                     |
| RBAC and audit | Seeds the permission catalog on every boot and writes the append-only audit log.                                                                                                                                   |
| Job workers    | River job queue and its workers (metering, billing, snapshots, storage lifecycle, plugin jobs). River runs inside the same process and stores jobs in PostgreSQL. The River web UI is mounted at `/admin/jobs/ui`. |
| Plugin runtime | The wazero WASM runtime that runs installed plugins.                                                                                                                                                               |
| Providers      | Drivers for Incus (compute), PowerDNS (DNS) and SeaweedFS (object storage).                                                                                                                                        |

The binary does **not** serve the dashboard. Caddy serves the built single-page app from `web/dist` and forwards only backend paths to Lahijan.

Each provider is optional. When `providers.incus.enabled`, `providers.powerdns.enabled` or `providers.seaweedfs.enabled` is false, Lahijan skips that driver and the matching API routes answer `501` with a "feature disabled" error. The production compose file turns all three on.

## Compose services

The production stack is `deployments/docker-compose.prod.yml` (compose project name `lahijan-prod`). See [Deployment](/docs/operations/deployment) for images and resource limits.

| Service                                                           | Role                                                                                                        |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `caddy`                                                           | Public entry point. TLS, dashboard files, reverse proxy to Lahijan, Grafana and Jaeger.                     |
| `lahijan`                                                         | The Lahijan process.                                                                                        |
| `migrate`                                                         | One-shot golang-migrate job that applies database migrations before `lahijan` starts.                       |
| `postgres`                                                        | PostgreSQL 16 with three databases: `lahijan`, `pdns` and `seaweed` (names come from the environment file). |
| `powerdns`                                                        | PowerDNS Authoritative with the `gpgsql` backend and its HTTP API.                                          |
| `seaweed-master`, `seaweed-volume`, `seaweed-filer`, `seaweed-s3` | SeaweedFS split into master, volume server, filer (metadata in PostgreSQL) and S3 gateway.                  |
| `seaweed-iam-init`                                                | One-shot job that seeds the S3 admin identity in the filer if none exists.                                  |
| `incus`                                                           | The Incus daemon, running privileged with host networking.                                                  |
| `otel-collector`, `jaeger`, `loki`, `prometheus`, `grafana`       | Observability services. See [Observability](/docs/operations/observability).                                |
| `powerdns-recursor`, `dnsdist`                                    | Optional. Only start with the `dns-full` compose profile.                                                   |

## Networks and ports

The stack defines two bridge networks:

- `frontend` (`lahijan-prod-frontend`): only Caddy is attached.
- `backend` (`lahijan-prod-backend`): every other service except `incus`. Caddy is on both.

The `incus` service uses `network_mode: host`, so it is on neither network. Lahijan reaches it through the Unix socket, not over the network.

Ports published on the host:

| Port                                | Service                                   | Purpose                                     |
| ----------------------------------- | ----------------------------------------- | ------------------------------------------- |
| 80/tcp, 443/tcp, 443/udp            | `caddy`                                   | HTTP (redirects to HTTPS), HTTPS and HTTP/3 |
| 53/udp, 53/tcp                      | `powerdns` (or `dnsdist` with `dns-full`) | Authoritative DNS                           |
| 8333/tcp (`SEAWEEDFS_S3_HOST_PORT`) | `seaweed-s3`                              | S3 data plane                               |

Internal-only ports on the `backend` network include `lahijan:8080`, `powerdns:8081` (HTTP API), `seaweed-master:9333`, `seaweed-volume:8080`, `seaweed-filer:8888`, `seaweed-s3:8333`, `postgres:5432` and `otel-collector:4317`/`4318`.

## Request flow

### Dashboard and API

1. A browser loads `https://app.example.com/`. Caddy serves files from `web/dist`. Unknown paths fall back to `index.html`.
2. Requests to `/api/*`, `/healthcheck/*` and `/admin/jobs/ui` go to `lahijan:8080`. Caddy flushes responses immediately, so streams and WebSockets (the instance console) pass through.
3. Inside Lahijan the middleware order is request id, recover, CORS, logger, tenant, auth, audit and RBAC, then the handler.

### Compute

Lahijan talks to Incus over the Unix socket `/var/lib/incus/unix.socket`. The `incus` container writes its state to `/var/lib/incus` on the host, and the `lahijan` container mounts the same host path read-only. Each tenant maps to an Incus project named `lahijan-tenant-<tenant-uuid>` (the prefix is `providers.incus.projectPrefix`). The socket is never published on the network.

### DNS

Lahijan calls the PowerDNS HTTP API at `http://powerdns:8081` with the shared `PDNS_API_KEY`. PowerDNS stores zones in the `pdns` PostgreSQL database and answers queries on port 53. Lahijan is the only writer (`dnsupdate=no`).

### Object storage

Control-plane actions (buckets, access keys, quotas) go through Lahijan, which uses the S3 gateway at `http://seaweed-s3:8333` and the filer at `http://seaweed-filer:8888` with the admin key pair. Lahijan writes every minted access key into the filer document `/etc/iam/identity.json`, which `weed s3` reloads on change. Users' S3 clients and browsers send object data straight to the S3 gateway, not through Lahijan. Pre-signed URLs are signed for `LAHIJAN_S3_PUBLIC_URL`.

## What is stateful

| Data                                                                           | Where it lives                                                       | Notes                                               |
| ------------------------------------------------------------------------------ | -------------------------------------------------------------------- | --------------------------------------------------- |
| Lahijan data (users, tenants, audit log, billing ledger, jobs, plugin modules) | `lahijan` database in volume `lahijan-prod-postgres`                 | Plugin `.wasm` bytes are stored in PostgreSQL.      |
| DNS zones and records                                                          | `pdns` database in the same volume                                   |                                                     |
| SeaweedFS file metadata and S3 identities                                      | `seaweed` database in the same volume                                |                                                     |
| Object data                                                                    | Volume `lahijan-prod-seaweedfs`                                      | Mounted by the master and volume server containers. |
| Incus instances, images, storage pools, Incus database                         | Host directory `/var/lib/incus`                                      | A bind mount, not a named volume.                   |
| TLS certificates and ACME account                                              | Volumes `lahijan-prod-caddy-data`, `lahijan-prod-caddy-config`       | Caddy re-issues if lost.                            |
| Metrics and dashboards                                                         | Volumes `lahijan-prod-prometheus`, `lahijan-prod-grafana`            | Jaeger keeps traces in memory only.                 |
| Plugin volume                                                                  | Volume `lahijan-prod-plugins`, mounted at `/var/lib/lahijan/plugins` |                                                     |
| Secrets                                                                        | `deployments/.env.prod`                                              | Not in any volume. Keep a separate copy.            |

The `lahijan` process itself keeps no state between restarts. Sessions, jobs and plugin state are all in PostgreSQL.

> [!NOTE]
> WASI plugins keep files under `wasm.wasi.fsRoot` (default `/var/lib/lahijan/wasi-fs`). The shipped compose file does not mount a volume there, so those files are lost when the container is recreated.

## Single node

The shipped stack runs on one host. PostgreSQL, PowerDNS and the single SeaweedFS volume server are not replicated. Lahijan can place instances across an Incus cluster (see [Compute cluster](/docs/admin/compute-cluster)), but the compose file itself does not set that up.
