# ADR-0006: Managed dependencies — Lahijan ships the whole stack

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan depends on three external services: Incus, PowerDNS Authoritative, and
SeaweedFS. Deployers must be able to install Lahijan without separately
provisioning each backend.

Options considered:

- **Bring your own** — deployer installs + configures Incus, PDNS, SeaweedFS;
  Lahijan connects via API. Maximum flexibility; high friction.
- **Managed by Lahijan** — Lahijan ships a docker-compose stack + install
  scripts that bring up everything. One-command install.
- **Hybrid** — Lahijan ships the stack but auto-detects existing services if
  present.

## Decision

Lahijan ships a **fully managed docker-compose stack**. The default
`docker compose up` brings up:

- Postgres (shared by Lahijan, PDNS, SeaweedFS Filer — see ADR-0007)
- Incus client container + host-installed Incus daemon
- PowerDNS Authoritative (gpgsql backend)
- SeaweedFS (master + volume + filer + s3 in prod; `weed mini` in dev)
- OTel collector + Jaeger + Loki + Prometheus
- Lahijan itself

Power users can still point Lahijan at existing services via env vars, but the
default experience is one command.

## Consequences

- **Positive:** one-command install; great OSS UX; reproducible dev envs.
- **Positive:** CI mirrors production exactly.
- **Negative:** Lahijan takes operational responsibility for backends it didn't write.
- **Negative:** upgrades must coordinate across all services; documented runbook required.

## Compliance

- `deployments/docker-compose.dev.yml` and `deployments/docker-compose.prod.yml`
  bring up the full stack.
- `deployments/README.md` is the operator guide.
- Each service ships a `HEALTHCHECK`.
- Versions are pinned in the compose file; never `latest`.

## References

- ADR-0005 (single-host topology)
- ADR-0007 (shared Postgres)
- `deployments/AGENTS.md`
- WS-23 (production deployment & ops)
