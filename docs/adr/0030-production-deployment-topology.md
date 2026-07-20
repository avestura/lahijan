# ADR-0030: Production deployment topology — Caddy, single-host, four-script ops

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** maintainer

## Context

WS-23 settles Lahijan's production deployment story. The earlier ADRs
(0005 single-host, 0006 managed deps, 0007 shared Postgres) left three
specific shape questions open:

1. **Reverse proxy:** the WS-23 doc lists "Caddy or Traefik? Default:
   Caddy — simpler config, automatic TLS, no extra deps." The choice
   was a default, not a decision. The implementation needs to commit.

2. **Single-binary deployment for homelab:** the doc says "Default: no —
   compose is the canonical form for MVP; bare binary is documented as
   advanced." Same shape: default, not decision.

3. **Backup target:** the doc says "Default: both — local file by
   default, S3 push if configured." Default, not decision.

This ADR commits all three. It also documents the four-script ops
surface (install / upgrade / backup / restore) and the first-run admin
bootstrap flow that the implementation ships.

## Options considered

### Reverse proxy

- **Caddy 2** — one-file config, automatic Let's Encrypt via ACME, HTTP/3
  out of the box, single static binary. Pros: dead-simple operator
  experience. Cons: smaller ecosystem than Traefik; less fine-grained
  routing (sufficient for Lahijan's needs).
- **Traefik 3** — labels-based config (very Docker-native), LetsEncrypt
  challenge via traefik-acme, mature dashboard. Pros: rich routing.
  Cons: more concepts to learn; labels-on-every-service adds noise;
  dashboard is a separate concern.
- **Nginx + Certbot** — the legacy default. Pros: ubiquitous. Cons:
  needs an external ACME helper (certbot container + cron); config
  syntax is heavier; HTTP/3 is a compile-time option in many distros.

**Decision: Caddy 2.** The operator-experience win (one file, automatic
TLS) is the deciding factor. Lahijan's routing surface is small (one
dashboard + REST API + a couple of admin UIs) so Traefik's richer
routing is not needed.

### Single-binary deployment

- **Compose only (the chosen option):** the canonical install is
  `docker compose -f ... up -d`. The bare-binary path is documented as
  advanced (the `init/` directory has a systemd unit for those who
  insist). Operators who want bare metal take on the responsibility of
  wiring Postgres, PowerDNS, and SeaweedFS themselves.
- **Compose + first-class bare binary:** ship both as equals. Rejected
  — it doubles the support matrix.
- **Bare binary only:** ship the binary + let the operator bring their
  own Postgres/PDNS/SeaweedFS. Rejected — violates ADR-0006 (Lahijan
  ships the whole stack).

**Decision: Compose only as canonical; bare binary in `init/` is
documented for advanced users.**

### Backup target

- **Local only:** simplest. Rejected — disk failure takes the backup
  with the live data.
- **S3 only:** resilient. Rejected — operator cannot restore without
  network access.
- **Both (the chosen option):** write to local dir by default; push to
  S3 when configured. The operator picks the redundancy level.

**Decision: both.** `scripts/backup.sh` writes locally + (when
`LAHIJAN_BACKUP_S3_BUCKET` is set) uploads via aws-cli.

## Decision

1. **Reverse proxy:** Caddy 2.8 (alpine variant) is the only reverse
   proxy in the prod compose. The Caddyfile lives at
   `deployments/caddy/Caddyfile` and is templated from
   `$LAHIJAN_PUBLIC_HOST`. Operator-only routes (`/grafana`, `/jaeger`)
   are gated by basic auth via Caddy's `basic_auth` directive.

2. **Single-host compose is canonical.** `deployments/docker-compose.prod.yml`
   is the supported install path. `init/lahijan.service` is a thin
   systemd unit for advanced bare-metal operators; it is not tested in
   CI.

3. **Backup target:** local by default + optional S3 push. The retention
   knob (`LAHIJAN_BACKUP_RETENTION_DAYS`) prunes both stores.

4. **Four-script ops surface** (the recommended way to operate Lahijan):
   - `scripts/install.sh` / `scripts/install.ps1` — first install.
   - `scripts/upgrade.sh` — pull latest + restart in dependency order.
   - `scripts/backup.sh` — pg_dump + SeaweedFS volume snapshot.
   - `scripts/restore.sh` — restore from a backup tarball.

5. **First-run admin bootstrap** (the new Go package
   `internal/app/lahijan/bootstrap`): on a fresh DB, `program.Start`
   creates a default tenant + a `platform.admin` user from
   `$LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` and either
   `$LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD` (operator-chosen) or a 24-char
   random password (logged ONCE via slog at WARN). The bootstrap is
   idempotent: it skips silently as soon as any user exists.

## Consequences

- **Positive:** the operator experience is one command (`install.sh`)
  + one script per daily op (upgrade / backup / restore). The Caddy
  choice removes an entire class of "how do I get a cert" questions.
- **Positive:** the first-run admin bootstrap removes the "you forgot
  to seed an admin" failure mode that catches every fresh self-hosted
  SaaS deploy.
- **Negative:** Caddy + Jaeger + Loki + Prometheus + Grafana all add
  memory. The total observability stack idle is ~1.5 GB. The default
  resource limits in the compose reflect this; very small hosts (2 GB
  RAM) need to disable the obs stack via a compose override.
- **Negative:** the four-script surface is bash (with a PowerShell
  sibling for install). Bash has its share of portability pitfalls;
  the scripts use `set -euo pipefail` + `bash -n` CI validation (TBD
  as a follow-up) but cannot match a Go-based runner for cross-platform
  robustness. The choice was deliberate: scripts are cheap to read +
  patch in a way a Go binary is not.

## Compliance

- `deployments/docker-compose.prod.yml` ships one `caddy` service +
  the four-script surface in `scripts/`.
- `deployments/caddy/Caddyfile` is the only reverse proxy config.
- `deployments/.env.prod.example` documents every required var.
- `internal/app/lahijan/bootstrap` is the only first-run admin flow.
- The `init/lahijan.service` systemd unit exists for bare-metal
  operators; the `install-service.sh` script wraps it.
- `deployments/README.md` is the operator's front door.
- `deployments/SECRETS.md` documents every secret + its rotation
  procedure.

## References

- WS-23 doc (`docs/workstreams/WS-23-production-deployment-ops.md`)
- ADR-0005 (single-host topology)
- ADR-0006 (Lahijan ships the whole stack)
- ADR-0007 (shared Postgres)
- ADR-0011 (direct S3 for data)
- ADR-0016 (full OTel)
- ADR-0029 (test sandbox topology — informs the prod shape)
