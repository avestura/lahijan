# Lahijan — Operator Guide

This guide walks an external operator through installing, running,
upgrading, and recovering Lahijan in production. It is the canonical
reference for the docker-compose-based deploy topology decided in
ADR-0005 + ADR-0006 + ADR-0007 + ADR-0030.

> **TL;DR** — install on a Linux host with Docker + Incus installed:
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/avestura/lahijan/main/scripts/install.sh | bash
> ```

## 1. Architecture

Lahijan ships a single-host, all-in-one Docker Compose stack. One
command brings up:

| Service | Container | Purpose |
|---------|-----------|---------|
| `caddy` | `caddy:2.8-alpine` | Public ingress, TLS termination, auto-Letsencrypt |
| `lahijan` | `ghcr.io/avestura/lahijan:<tag>` | The Go backend (REST API + dashboard SPA + River workers) |
| `postgres` | `postgres:16-alpine` | Shared Postgres (3 logical DBs: lahijan, pdns, seaweed) |
| `powerdns` | `powerdns/pdns-auth-49:4.9.3` | Authoritative DNS, gpgsql backend |
| `seaweed-master/volume/filer/s3` | `chrislusf/seaweedfs:3.61` | Object storage (prod split) |
| `incus-client` | `ghcr.io/lxc/incus:6.0` | Sidecar that surfaces the host Incus socket |
| `otel-collector` | `otel/opentelemetry-collector-contrib:0.108.0` | Telemetry fan-out hub |
| `jaeger/loki/prometheus/grafana` | upstream images | Observability backends |

Caddy is the only service with public ports (80 + 443). Everything else
lives on the `backend` overlay network. **Incus runs on the host** (not
in a container) because it needs kernel access; the Lahijan container
reaches it via a read-only bind-mount of the host socket.

## 2. Prerequisites

### 2.1 Host

- **Linux** with kernel ≥ 5.15 (Incus minimum). Ubuntu 22.04+ or Debian
  12+ recommended. macOS / Windows hosts cannot run Incus and will get
  a stack with the compute module disabled.
- **4 vCPU / 8 GB RAM / 50 GB disk** as a comfortable floor. Smaller
  hosts (2 vCPU / 4 GB) work for a personal / homelab deploy.
- **Public IP + ports 80 + 443 reachable** for the dashboard to be
  public-facing + Let's Encrypt to issue. Pure-LAN deploys work but TLS
  requires an internal CA (see §10).
- **TCP/UDP 53 reachable** for the DNS module to be authoritative on a
  public zone.

### 2.2 Software

Install in this order:

1. **Docker Engine 24+** with the `docker compose` subcommand (v2 is
   the default with modern Docker). Verify:
   ```bash
   docker version
   docker compose version
   ```
2. **Incus 6.0+** (Linux only). Follow the upstream guide:
   https://linuxcontainers.org/incus/docs/main/installing/ — snap install
   is the easiest path on Ubuntu.
3. **git** for `install.sh` to clone the repo.
4. **openssl** (for secret generation — `install.sh` will invoke it).
5. **(optional) aws-cli** when pushing backups to S3.

### 2.3 DNS

Before `install.sh` runs, point an A record at the host's public IP:

- `app.example.com  A  <host-ip>` (the dashboard hostname)

Optional additional records:

- `s3.example.com  A  <host-ip>` (virtual-host-style S3 endpoint)
- `ns1.example.com A  <host-ip>` (the host becomes authoritative for
  `example.com` — see PowerDNS §11)

## 3. Install

### 3.1 One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/avestura/lahijan/main/scripts/install.sh | bash
```

The one-liner clones the repo into `/opt/lahijan` (Linux) or `~/lahijan`
(macOS), renders `deployments/.env.prod` from the example, and brings
the stack up.

### 3.2 Interactive

```bash
git clone https://github.com/avestura/lahijan.git /opt/lahijan
cd /opt/lahijan
scripts/install.sh --install-dir /opt/lahijan
```

The script prompts for every required secret. Generated values are
written to `deployments/.env.prod` (which is gitignored). At the end it
prints the first-run admin credentials — **copy them now**, they are
shown only once.

### 3.3 Windows

Windows cannot run Incus locally. The PowerShell installer is for
dev-style validation of the prod topology on a Windows workstation
where the operator has Docker Desktop:

```powershell
git clone https://github.com/avestura/lahijan.git $env:USERPROFILE\lahijan
cd $env:USERPROFILE\lahijan
.\scripts\install.ps1
```

For a real production deploy, run `install.sh` on the Linux host.

## 4. First-run admin

On the very first boot of a fresh database, Lahijan's bootstrap package
(WS-23) creates a `platform.admin` user + a default tenant. The credentials
are:

- **email:** the value of `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` you set during
  install.
- **password:** either the value of `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD`
  (when you pre-set it) or a freshly-generated 24-char random string
  logged via `docker compose logs lahijan`. The log line looks like:

  ```
 lahijan-prod-app | 2026/07/20 14:30:00 WARN bootstrap: first-run admin
 credentials (change immediately) ... password=<24-char-string> ...
  ```

**Rotate the password immediately** via the dashboard's profile page.
The bootstrap runs once per database; it will skip silently on every
subsequent boot.

To re-run the bootstrap on an existing stack, drop the database (see
§9 Restore) or `DELETE FROM users` and restart Lahijan.

## 5. Daily ops

### 5.1 Logs

```bash
# Tail every service's logs:
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs -f

# Tail just Lahijan:
docker compose ... logs -f lahijan

# Tail just Caddy (TLS issuance + access logs):
docker compose ... logs -f caddy
```

The `x-logging` anchor in the compose file caps every container at
**10 MB / 3 files** so a chatty service cannot fill the disk. For longer
retention, ship logs via Promtail (Loki) or Fluent Bit.

### 5.2 Metrics

Prometheus + Grafana are wired into the stack. Browse to
`https://<host>/grafana` and log in with the operator credentials you
set during install. The dashboard ships Lahijan's metrics via the OTel
collector → Prometheus.

### 5.3 Health

Every container ships a healthcheck. The summary view:

```bash
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps
# STATUS column shows "(healthy)" once the probe passes.
```

The Lahijan app's healthcheck endpoint is
`http://localhost:8080/healthcheck/liveness` (inside the container).
Caddy's external healthcheck is `https://<host>/healthcheck/liveness`
via the reverse proxy.

### 5.4 Backup

```bash
# Manual backup (writes to $LAHIJAN_BACKUP_LOCAL_DIR):
/opt/lahijan/scripts/backup.sh

# Cron entry (02:00 daily, root):
0 2 * * * /opt/lahijan/scripts/backup.sh >> /var/log/lahijan-backup.log 2>&1
```

Set `LAHIJAN_BACKUP_S3_BUCKET` in `.env.prod` to additionally push to
S3-compatible storage. See §6.

### 5.5 S3 credentials for end users

Users mint their own S3 credentials via the dashboard (Storage → Buckets
→ Credentials). Each credential is scoped to a single bucket. Users hit
SeaweedFS directly for data (per ADR-0011) — the endpoint URL is
`https://<host>:8333` (or whatever `SEAWEEDFS_S3_HOST_PORT` you
configured).

## 6. Backup + restore

The backup script produces a single tarball with:

- `pg_dump -Fc` of each logical DB (`lahijan`, `pdns`, `seaweed`).
- `pg_dumpall --roles-only` of the global roles.
- A `tar -czf` snapshot of the SeaweedFS volume.
- A `tar -czf` snapshot of the Lahijan plugin uploads volume.
- A `MANIFEST` with the image tags + timestamps.

The script does **not** dump the auth signing key + encryption key —
those are operator-managed (see `SECRETS.md`). The tarball alone is not
enough to restore a working deploy if those keys are lost.

```bash
# Backup
scripts/backup.sh

# Restore (interactive — asks for the public hostname as confirmation)
scripts/restore.sh --backup /path/to/lahijan-backup-*.tar.gz

# Restore (automated, skip prompt)
scripts/restore.sh --yes --backup /path/to/lahijan-backup-*.tar.gz
```

The restore script stops the Lahijan stack, drops + recreates the three
databases, restores each dump, replaces the SeaweedFS + plugins volumes,
and prints the next-step instructions.

**Test the round-trip on a fresh VM at least once before relying on it
for production recovery.** Backup + restore is a WS-23 DoD item; we
test the script syntax but cannot exercise every failure mode.

## 7. Upgrade

```bash
# Pull latest stable:
scripts/upgrade.sh

# Pin to a specific tag:
scripts/upgrade.sh --tag v0.2.0
```

The upgrade script:

1. Verifies the current Lahijan container is healthy (refuses otherwise
   unless `--force`).
2. Pulls the latest git ref + the new image.
3. Restarts `otel-collector`, then `lahijan` (which runs migrations on
   boot), then waits for the healthcheck, then restarts `caddy` to flip
   the proxy.
4. Prints the rollback instructions.

Rollback: set `LAHIJAN_IMAGE_TAG=<old-tag>` in `.env.prod` and re-run
`upgrade.sh`. Database down-migrations are paired with up-migrations
(golang-migrate, reversible per ADR-0003); they run automatically on
the older tag's bootstrap.

## 8. Reverse proxy details (Caddy)

Caddy was chosen over Traefik in ADR-0030 for the prod topology:
one-file config, automatic Let's Encrypt, no extra ACME helper.

The Caddyfile lives at `deployments/caddy/Caddyfile`. The site block is
templated from `$LAHIJAN_PUBLIC_HOST` so the operator only sets one env
var. Routes:

| Path | Upstream | Notes |
|------|----------|-------|
| `/` (everything not matched below) | `lahijan:8080` | Dashboard SPA + REST API |
| `/grafana` | `grafana:3000` | Operator-only (basic auth) |
| `/jaeger` | `jaeger:16686` | Operator-only (basic auth) |

Operator-only routes are gated by basic auth. **Replace the default
`admin/admin` hash** in `.env.prod` by running:

```bash
docker run --rm caddy:2.8-alpine caddy hash-password
# paste the bcrypt hash into GRAFANA_BASIC_AUTH_HASH + JAEGER_BASIC_AUTH_HASH
docker compose ... up -d --force-recreate caddy
```

## 9. Troubleshooting matrix

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `install.sh` hangs on "Waiting for Lahijan to become healthy" | Migrations slow on first boot OR a config error | `docker compose logs lahijan` — look for `failed to seed rbac catalog` or `failed to setup config` |
| Caddy shows 502 / "bad gateway" | Lahijan container not healthy | `docker inspect --format='{{json .State.Health.Status}}' lahijan-prod-app` |
| Browser shows cert warning | DNS for `LAHIJAN_PUBLIC_HOST` not pointing at host OR Let's Encrypt rate-limited | Check `dig app.example.com` + Caddy's logs (`docker compose logs caddy`); ACME errors are loud |
| Users cannot log in | Auth signing key changed (cookies invalidated) OR Postgres down | Check `docker compose logs lahijan` for `auth/signing.key must be set` |
| Compute module shows "feature disabled" | Incus socket not bind-mounted correctly | `docker exec lahijan-prod-app ls -la /var/lib/incus/unix.socket` — should exist |
| PowerDNS API key invalid | Mismatch between `PDNS_API_KEY` and `LAHIJAN_PROVIDERS_POWERDNS_API_KEY` (same value in two env vars) | Diff `.env.prod`; the two keys MUST be identical |
| SeaweedFS admin credentials rejected | Same mismatch shape (`SEAWEEDFS_S3_*` ↔ `LAHIJAN_PROVIDERS_SEAWEEDFS_ADMIN_*`) | Same fix |
| First-run admin not created | DB not actually empty OR `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` empty | `docker exec lahijan-prod-postgres psql -U postgres -d lahijan -c 'SELECT count(*) FROM users;'` — 0 means bootstrap should have run |
| `s3.example.com` not reachable | Caddy's optional site block needs `LAHIJAN_S3_PUBLIC_HOST` set + DNS pointing at the host | Set the env var + restart caddy |

## 10. Private / LAN deploys

Caddy's auto-Letsencrypt needs a public hostname + ports 80/443 reachable
from the internet. For a LAN-only deploy:

1. Use Caddy's `internal` TLS directive (self-signed CA). Edit
   `deployments/caddy/Caddyfile.local` (Caddy's `include_local` snippet
   picks it up) and add:
   ```
   :443 {
     tls internal
     reverse_proxy lahijan:8080
   }
   ```
2. Distribute the Caddy root CA to your clients (`docker cp` the cert
   from `/data/caddy/pki/authorities/local/root.crt`).
3. Re-create Caddy: `docker compose ... up -d --force-recreate caddy`.

This is documented but not the default — Let's Encrypt is the
recommended path.

## 11. Authoritative DNS (PowerDNS)

Lahijan ships PowerDNS Authoritative in the stack. To make a zone
authoritative via Lahijan:

1. At your registrar, delegate the zone to the host (set the NS records
   to point at this host's public hostname, e.g.
   `ns1.example.com → <host-ip>`).
2. Open TCP/UDP 53 on the host firewall (the compose publishes them).
3. In the Lahijan dashboard, create the zone. Lahijan writes the SOA +
   NS RRsets via the PDNS HTTP API.
4. Verify with `dig @<host-ip> <zone> SOA`.

Lahijan is the only writer to PDNS (dnsupdate is off). AXFR is off by
default; enable per-zone via the dashboard when adding a secondary NS.

## 12. Files

```
deployments/
  README.md                          # this file
  SECRETS.md                         # operator-managed secrets + rotation
  AGENTS.md                          # AI-context guide for this dir
  .env.example                       # dev env (compose.dev.yml)
  .env.prod.example                  # prod env (compose.prod.yml)
  docker-compose.dev.yml             # dev stack
  docker-compose.test.yml            # CI / sandbox stack (WS-22)
  docker-compose.prod.yml            # prod stack (WS-23)
  caddy/
    Caddyfile                        # prod TLS reverse proxy
  incus/
    preseed.yaml                     # `incus admin init --preseed` template
    README.md                        # host setup instructions
  powerdns/
    pdns.conf.template               # bare-metal PDNS reference config
    schema.pgsql.sql                 # gpgsql schema applied at first boot
    init.sh                          # the apply wrapper
  postgres/
    init.sh                          # creates the 3 logical DBs + roles
  prometheus/
    prometheus.yml                   # scrape config (collector + self)
  seaweedfs/
    s3.json                          # admin identity baked in for dev
  otel/
    otel-collector-config.yaml       # collector → Jaeger + Loki + Prometheus
scripts/
  install.sh / install.ps1           # one-shot bootstrap
  upgrade.sh                         # pull latest + restart in order
  backup.sh                          # pg_dump + SeaweedFS snapshot
  restore.sh                         # restore from a backup tarball
  run-e2e.sh                         # the e2e harness driver (WS-22)
```

## 13. Reference: ADRs

- ADR-0005 — single-host topology, multi-host-ready
- ADR-0006 — Lahijan ships the whole stack
- ADR-0007 — shared Postgres, separate logical DBs
- ADR-0008 — River (PostgreSQL-native) for jobs
- ADR-0009 — multi-node-ready from day 1
- ADR-0010 — full Incus surface
- ADR-0011 — direct S3 for data + Lahijan for control plane
- ADR-0016 — full OTel from day 1
- ADR-0029 — test sandbox topology
- ADR-0030 — production deployment topology (this WS)
- ADR-0033 — multi-node cluster design (WS-26)

## 14. Multi-node / HA deployments (WS-26)

Lahijan's architecture is multi-node-ready from day 1 (ADR-0009); WS-26
makes it real. Two scale-out shapes are supported:

### 14a. Multiple Lahijan replicas (HA control plane)

Run N replicas of the Lahijan container against one shared Postgres.
Sessions, audit, billing, jobs, and WASM state are all DB-backed; River
serialises job dispatch; no in-process state survives a restart. Useful
for blue/green deploys and zero-downtime upgrades.

Topology:

```
                    ┌──────────────┐
                    │   Caddy LB   │   (sticky not required)
                    └──────┬───────┘
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
     ┌──────────┐    ┌──────────┐    ┌──────────┐
     │ Lahijan 1│    │ Lahijan 2│    │ Lahijan N│
     └────┬─────┘    └────┬─────┘    └────┬─────┘
          │                │                │
          └────────┬───────┴────────────────┘
                   ▼
            ┌────────────┐
            │  Postgres  │   (HA via Patroni or managed PG)
            └────────────┘

  (Incus + PowerDNS + SeaweedFS stay where they are; Lahijan points at
  them via env, same as single-host.)
```

Steps:

1. Provision one Postgres for every replica to share (use a managed
   Postgres or a Patroni-managed cluster; the single-Postgres
   container from this stack is **not** HA).
2. Provision N Lahijan hosts; on each, install Docker + clone this
   repo.
3. On each host, copy `.env.prod.example` to `.env` and fill in the
   **same** values (same `LAHIJAN_DB_*`, same `LAHIJAN_AUTH_*`).
4. Run `scripts/install.sh` on each host. Each replica starts
   independently; River's leadership election serialises the job
   queue.
5. Front the replicas with a layer-7 LB (Caddy with multiple
   `reverse_proxy` targets, AWS ALB, Cloudflare, ...). Sessions are
   DB-backed so any LB algorithm works; pick `round_robin` for
   simplicity.

The single-host `docker-compose.prod.yml` ships one Lahijan replica;
to run N replicas use a compose override file (`docker-compose.override.yml`
is auto-merged):

```yaml
# docker-compose.override.yml
services:
  lahijan:
    deploy:
      replicas: 3
```

### 14b. Incus cluster (compute scale-out)

Multiple Incus hosts join one Incus cluster; Lahijan's
`ClusterPlacementDriver` queries the cluster API and places new
instances on the least-loaded member. Existing instances can be
live-migrated between members via `POST /api/v1/compute/instances/{id}/migrate`.

Steps:

1. Bootstrap an Incus cluster per the upstream docs
   (https://linuxcontainers.org/incus/docs/main/howto/cluster/).
   Every member must run the same Incus version; the cluster's
   dqlite database needs an odd number of voters (typically 3).
2. Verify the cluster is reachable from every Lahijan replica via
   the configured `INCUS_SOCKET_PATH` (Unix socket) or
   `INCUS_REMOTE_URL` (HTTPS remote).
3. Flip `providers.incus.placement.mode` from `local` to `cluster`
   in `.env`:

   ```
   LAHIJAN_PROVIDERS_INCUS_PLACEMENT_MODE=cluster
   ```

4. Restart every Lahijan replica. The first `GET /health` after
   restart reports the cluster mode; the admin UI's "Cluster" panel
   lists every member.

The `ClusterPlacementDriver` makes scheduling decisions under a per-
tenant Postgres advisory lock (`pg_advisory_xact_lock`) so concurrent
`CreateInstance` calls across replicas do not both pick the same
"least loaded" member. Per-instance placement metadata is mirrored
into the `compute_instances.cluster_member` column for the UI.

### 14c. What still needs to be single-node

- **PowerDNS Authoritative:** PDNS supports its own native replication
  (via `also-notify` + AXFR) but the single-container deployment in
  this stack is single-node. A future WS may ship a multi-master PDNS
  topology.
- **SeaweedFS:** runs `master + volume + filer + s3` as separate
  services in this stack; the topology is HA-friendly but the
  defaults are sized for a single host. Scale by adding volume
  servers.
- **Incus client sidecar:** the `incus-client` container in this
  stack is a thin wrapper around the host's Incus socket; it has no
  state and can be replicated freely.

### 14d. Public IP / floating IP data plane (WS-30, ADR-0037)

WS-30 ships the control plane for public-IP assignment (IP pools +
per-tenant floating IPs + best-effort Incus network-forwards). The
**data plane** — actually routing public IPs to your host — is the
operator's concern. Lahijan does NOT manage BGP, FRR, or NAT; it only
records the allocation in Postgres and opportunistically pushes an
Incus `network forward` when one is configured.

Two deployment shapes:

1. **Self-hoster with a single public IP.** You do not need this
   section. Leave `providers.incus.floatingIPs.forwardNetwork` empty;
   every floating-IP attach records `forward_push_status=
   "unsupported"`. The operator UI shows the allocation; the
   user's instance is reachable via the existing bridge / proxy
   device you already configured.

2. **SaaS-style deployment with a public IP range.** The operator
   owns a CIDR (RIR-allocated or provider-assigned) and wants
   Lahijan to hand out individual addresses.

   **Required operator setup (out of band from Lahijan):**

   - Route the CIDR to the Incus host(s). Typical options:
     - **BGP via FRR** (recommended for multi-host clusters):
       peer with your upstream and announce the CIDR from every
       Incus host. The host then accepts traffic for any IP in
       the CIDR.
     - **Static route:** your upstream router forwards the entire
       CIDR to the Incus host's primary IP. Fine for single-host.
   - Create a managed Incus network with `ipv4.address=` set to
     the operator's CIDR (Incus then owns the addresses inside
     that network and the network-forward API works).
   - Configure Lahijan to push forwards onto that network:

     ```
     LAHIJAN_PROVIDERS_INCUS_FLOATINGIPS_FORWARDNETWORK=lan-public
     ```

     (Replace `lan-public` with the name of the managed network you
     created above.) Every floating-IP attach now pushes an Incus
     `network forward` that maps the public IP to the instance.

   **Limitations:**

   - Per-IP rate limiting + abuse handling are NOT shipped with
     Lahijan. Add host-level enforcement (nftables / xtables / an
     external IDMS) if your deployment is exposed to the public
     internet.
   - Egress metering (per-GB charging) is NOT shipped. Per-IP-hour
     charging is wired via the WS-17 meter seam and will be
     emitted by a follow-up WS's periodic worker.

   See `docs/adr/0037-public-ip-floating-ips-design.md` for the
   full design rationale.

## 15. Getting help

- **Issues:** https://github.com/avestura/lahijan/issues
- **Docs site:** https://lahijan.dev (rendered from `/docs`)
- **Glossary:** `docs/glossary.md`
