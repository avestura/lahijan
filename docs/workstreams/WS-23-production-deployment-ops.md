# WS-23 · Production Deployment & Ops

```
Status: pending
Phase: 6
Depends on: WS-22
Unblocks: WS-26 (multi-node), public release
```

## Goal

Make Lahijan safely deployable by an external operator: a production
docker-compose, install scripts, secrets handling, upgrade path, backup +
restore runbooks. After this WS, a new operator can self-host Lahijan in under
an hour.

## Scope

**In scope:**
- `deployments/docker-compose.prod.yml` — pinned versions, TLS, persistent
  volumes, resource limits, healthchecks, logging driver
- `deployments/.env.prod.example` — every required var documented
- `deployments/caddy/` or `deployments/traefik/` — TLS-terminating reverse
  proxy with auto-Letsencrypt (pick one; default: Caddy for simplicity)
- `scripts/install.sh` (Linux/macOS) + `scripts/install.ps1` (Windows) —
  bootstrap: check prereqs, clone repo, render env, `docker compose up`,
  seed admin user, print URLs
- `scripts/upgrade.sh` — pull latest, run migrations, restart services in
  dependency order
- `scripts/backup.sh` — pg_dump all three databases + SeaweedFS volume
  snapshots; writes to a configurable target (local, S3-compatible)
- `scripts/restore.sh` — restore from a backup
- `deployments/README.md` — operator guide:
  - prerequisites (host with Incus installed, Docker, public IP if you want
    public-facing services)
  - install steps
  - first-run admin bootstrap
  - daily ops: logs, metrics, health
  - upgrade procedure
  - backup + restore
  - troubleshooting matrix
- systemd unit refresh (the existing `init/lahijan.service` is for bare-metal
  Lahijan; for compose-based install, document `docker compose up -d` as the
  service)
- Admin bootstrap: on first run with an empty DB, create a `platform.admin`
  user from env-configured email + a printed-one-time password
- Secrets:
  - AES-GCM master key for encrypted DB columns; loaded from env
  - per-service credentials (PDNS API key, SeaweedFS admin) in env
  - documented key-rotation procedure

**Out of scope:**
- Multi-node cluster topology (WS-26).
- k8s manifests (Phase 7).
- High-availability Postgres (use a managed Postgres for prod; document).

## Required reading for the AI session

- `/AGENTS.md`
- `deployments/AGENTS.md`
- `docs/adr-0005-mvp-topology.md`
- `docs/adr-0006-managed-dependencies.md`
- `docs/adr-0007-shared-postgres.md`
- All provider WSs (11, 12, 13) for their container/volume needs
- WS-22 doc (the test harness deployment pattern)

## Deliverables

- Production docker-compose with pinned versions + TLS + healthchecks
- Reverse proxy with auto-TLS
- Install / upgrade / backup / restore scripts
- Operator guide (deployments/README.md)
- Admin bootstrap flow
- Secrets handling documented

## Definition of Done

- [ ] a fresh Linux VM with Docker + Incus can run `scripts/install.sh` and
      have Lahijan live in under 1 hour
- [ ] TLS works end-to-end (browser accepts the cert)
- [ ] upgrade from N-1 to N preserves user data (tested)
- [ ] backup + restore round-trips (tested on a stack with sample data)
- [ ] first-run admin bootstrap creates a usable admin user
- [ ] every secret loaded from env; `.env.example` documents every var
- [ ] operator guide covers every daily op
- [ ] healthchecks for every container
- [ ] `make lint test` still green

## Open questions

- Reverse proxy: Caddy or Traefik? (Default: Caddy — simpler config, automatic
  TLS, no extra deps.)
- Single-binary deployment (no compose) for homelab? (Default: no — compose
  is the canonical form for MVP; bare binary is documented as advanced.)
- Backup target: S3-compatible, local file, or both? (Default: both — local
  file by default, S3 push if configured.)

## Notes

- Treat the install script as the first impression. Make it robust to common
  failure modes (no Docker, no Incus, wrong kernel, full disk).
- Document the host requirements clearly: Incus needs a recent Linux kernel;
  Windows/Mac deployers must use a Linux VM.
