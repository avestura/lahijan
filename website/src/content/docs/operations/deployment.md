---
title: Deployment
description: Run, inspect and size the production Docker Compose stack, and put it behind an existing reverse proxy.
---

This page covers day-to-day handling of the production stack in `deployments/docker-compose.prod.yml`: what each service is, where data is kept, and the commands to start, stop and inspect it. For a first install, follow [Install on a server](/docs/getting-started/installation) first.

All commands on this page run from the install directory (`/opt/lahijan` when you used `scripts/install.sh`). Every compose command needs both the environment file and the compose file:

```sh
cd /opt/lahijan
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps
```

## Before the first start

- The host must be Linux. The `incus` service needs a privileged container with host networking, host PID and host cgroup namespaces, plus `/dev`, `/var/lib/incus` and `/lib/modules` from the host.
- Copy `deployments/.env.prod.example` to `deployments/.env.prod` and replace every `CHANGEME` value. See [Environment file](/docs/operations/environment).
- Build the dashboard. Caddy serves `web/dist` from the checkout and the Lahijan binary does not serve it. Run `make web-build` (Node.js is needed) before `up`. `scripts/install.sh` does not do this for you.
- Point DNS at the host before the first start so Caddy can get a certificate. See [TLS and domains](/docs/operations/tls-and-domains).

## Services

| Service             | Image                                                 | Purpose                                                         |
| ------------------- | ----------------------------------------------------- | --------------------------------------------------------------- |
| `caddy`             | `caddy:2.8-alpine`                                    | TLS termination, dashboard files, reverse proxy                 |
| `postgres`          | `postgres:16-alpine`                                  | Shared PostgreSQL for Lahijan, PowerDNS and the SeaweedFS filer |
| `powerdns`          | `powerdns/pdns-auth-49:4.9.3`                         | Authoritative DNS and HTTP API                                  |
| `powerdns-recursor` | `powerdns/pdns-recursor-49:4.9.3`                     | Optional recursor (`dns-full` profile)                          |
| `dnsdist`           | `powerdns/dnsdist-17:1.7.7`                           | Optional DNS load balancer (`dns-full` profile)                 |
| `seaweed-master`    | `chrislusf/seaweedfs:${SEAWEEDFS_IMAGE_TAG:-3.99}`    | SeaweedFS master                                                |
| `seaweed-volume`    | same                                                  | SeaweedFS volume server                                         |
| `seaweed-filer`     | same                                                  | SeaweedFS filer, metadata in PostgreSQL                         |
| `seaweed-iam-init`  | `curlimages/curl:8.10.1`                              | One-shot: seeds the S3 admin identity                           |
| `seaweed-s3`        | same SeaweedFS image                                  | S3 gateway                                                      |
| `incus`             | `ghcr.io/cmspam/incus-docker:${INCUS_IMAGE_TAG:-lts}` | Incus daemon (6.0 LTS by default)                               |
| `migrate`           | `migrate/migrate:v4.19.1`                             | One-shot: applies database migrations                           |
| `lahijan`           | `ghcr.io/avestura/lahijan:${LAHIJAN_IMAGE_TAG}`       | The Lahijan API and job workers                                 |
| `otel-collector`    | `otel/opentelemetry-collector-contrib:0.108.0`        | OpenTelemetry collector                                         |
| `jaeger`            | `jaegertracing/all-in-one:1.60`                       | Trace storage and UI (in memory)                                |
| `loki`              | `grafana/loki:3.1.1`                                  | Log storage                                                     |
| `prometheus`        | `prom/prometheus:v2.54.1`                             | Metrics, 15 day retention                                       |
| `grafana`           | `grafana/grafana:11.2.0`                              | Dashboards, served at `/grafana`                                |

Start order is enforced with health checks: `lahijan` waits for `postgres`, `powerdns`, `seaweed-s3` and `incus` to be healthy and for `migrate` to exit successfully. Caddy waits for `lahijan`.

If you build the Lahijan image yourself, tag it with the name the compose file expects:

```sh
docker build --build-arg LAHIJAN_IMAGE_TAG=v0.1.0 -t ghcr.io/avestura/lahijan:v0.1.0 .
```

The build argument is stamped into the binary as its version.

## Volumes

| Compose volume    | Docker volume name          | Mounted in                         |
| ----------------- | --------------------------- | ---------------------------------- |
| `postgres_data`   | `lahijan-prod-postgres`     | `postgres`                         |
| `seaweedfs_data`  | `lahijan-prod-seaweedfs`    | `seaweed-master`, `seaweed-volume` |
| `caddy_data`      | `lahijan-prod-caddy-data`   | `caddy`                            |
| `caddy_config`    | `lahijan-prod-caddy-config` | `caddy`                            |
| `prometheus_data` | `lahijan-prod-prometheus`   | `prometheus`                       |
| `grafana_data`    | `lahijan-prod-grafana`      | `grafana`                          |
| `lahijan_plugins` | `lahijan-prod-plugins`      | `lahijan`                          |

Incus state is not a named volume. It lives in `/var/lib/incus` on the host.

`docker compose down` keeps all of this. `docker compose down -v` deletes the named volumes.

## Ports

Only three services publish ports: `caddy` (80/tcp, 443/tcp, 443/udp), `powerdns` (53/udp, 53/tcp) and `seaweed-s3` (`SEAWEEDFS_S3_HOST_PORT`, default 8333). The `incus` service uses host networking and creates its own bridge (`lahijanbr` after the preseed below). Everything else is only reachable on the internal `backend` network.

## Start, stop and inspect

```sh
# Start or update the whole stack
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml up -d

# Status and health of every container
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps

# Follow logs (all services, or one)
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs -f
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs -f lahijan

# Restart one service
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml restart caddy

# Recreate one service after changing .env.prod
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml up -d --force-recreate lahijan

# Stop everything (data is kept)
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml down
```

Every container logs with the `json-file` driver, rotated at 10 MB with 3 files kept.

On the very first boot, read the bootstrap admin password from the Lahijan log if you did not set `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD`:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs lahijan | grep bootstrap
```

### Initialize Incus once

After the first `up`, apply the preseed from inside the `incus` container. It creates the `lahijanbr` bridge (10.10.10.1/24 with NAT), a `dir` storage pool named `default` and a fallback profile:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml exec -T incus \
  incus admin init --preseed < deployments/incus/preseed.yaml
```

Lahijan attaches tenant instances to `lahijanbr` by default (`providers.incus.defaultNetwork`).

## Resource limits and sizing

The compose file sets CPU and memory limits on every long-running service. The largest are `incus` (2 CPUs, 2 GB), `lahijan` (2 CPUs, 1 GB), `postgres` (1 CPU, 1 GB) and `seaweed-volume` (1 CPU, 1 GB). The compose file notes that these defaults fit a 4 GB, 2 vCPU homelab host. The operator guide in `deployments/README.md` suggests 4 vCPU, 8 GB RAM and 50 GB of disk as a comfortable floor.

Keep in mind:

- Instances you run in Incus use host resources on top of these limits. The `incus` container limit applies to the daemon container.
- Docker refuses a CPU limit higher than the host's CPU count. On a host with fewer than 2 vCPUs, lower the `cpus: "2.0"` limits of `incus` and `lahijan` in a compose override file.
- SeaweedFS volumes can grow to `SEAWEEDFS_VOLUME_SIZE_LIMIT_MB` (default 30000 MB). Lower it on small disks.

## Scaling limits

This stack is single-node. There is one PostgreSQL container, one PowerDNS server, one SeaweedFS volume server (default replication `000`, no extra copies) and one Incus daemon. Raising `SEAWEEDFS_DEFAULT_REPLICATION` only works after you add volume servers. Lahijan can place instances across an Incus cluster (see [Compute cluster](/docs/admin/compute-cluster)), but building that cluster is outside the compose file.

## Behind an existing reverse proxy

The stack expects Caddy to own ports 80 and 443 and to get its own certificate. If another proxy already holds those ports, keep Caddy (it serves the dashboard files and routes the backend paths) and put your proxy in front of it:

1. Set `LAHIJAN_PUBLIC_HOST=http://app.example.com` in `.env.prod`. The `http://` scheme makes Caddy serve that site on plain HTTP without requesting a certificate.
2. In a compose override file, replace Caddy's published ports with a local one, for example `127.0.0.1:8088:80`. Docker Compose merges `ports` lists, so use the `!override` tag (Compose 2.24.4 or newer) to drop the defaults.
3. Point your proxy at `http://127.0.0.1:8088`, keep TLS on your proxy, pass the original `Host` header and allow WebSocket upgrades (the instance console needs them).
4. Keep `LAHIJAN_PUBLIC_URL` set to the public `https://` origin. Email links, the Grafana root URL and the S3 CORS origin use it.

```yaml title="deployments/docker-compose.override.yml"
services:
  caddy:
    ports: !override
      - "127.0.0.1:8088:80"
```

Pass the override with a second `-f deployments/docker-compose.override.yml` on every command.

> [!WARNING]
> This layout is not the tested default. Session cookies are always marked `Secure` in the production file (`LAHIJAN_AUTH_SESSION_SECURE=true`), so users must reach the dashboard over HTTPS on your proxy.
