# ADR-0007: Shared Postgres, separate logical DBs

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

PowerDNS Authoritative needs a backend store (we'll use gpgsql), and SeaweedFS
Filer needs a metadata store (Postgres is one option). Lahijan itself also
needs Postgres. Three Postgres instances is wasteful for a self-hosted deploy.

Options considered:

- **Separate Postgres instance per service** — strongest isolation; triple the
  RAM; triple the operational burden.
- **Shared Postgres instance, separate logical databases** — one container,
  three databases (`lahijan`, `pdns`, `seaweed`); separate connection pools;
  separate backups needed only if you want them.
- **Shared Postgres, shared schema** — table-prefix everything; risky;
  migration coupling.

## Decision

Use **one Postgres instance** with **three logical databases**: `lahijan`,
`pdns`, `seaweed`. Each service connects with its own credentials scoped to
its database only. In production, deployers may split them by changing
connection strings — that's a config change, not a code change.

River (job queue) lives in the `lahijan` database as the `river_*` schema.

## Consequences

- **Positive:** one container, one backup, one set of credentials to rotate.
- **Positive:** each service has its own DB user with only its own DB granted;
  no cross-service SQL.
- **Positive:** deployers can split later by editing compose env.
- **Negative:** a single misbehaving service can starve the others for
  connections; mitigated with per-DB connection pool limits.
- **Negative:** Postgres is a SPOF until WS-26 (multi-node) lands.

## Compliance

- `deployments/docker-compose.*.yml` declares one `postgres` service.
- Init scripts create the three databases and three users.
- Each service's config (`PDNS_DATABASE_NAME=...`, etc.) points at its own DB.
- Lahijan's `conf` has separate `database.*` keys for its own connection.

## References

- ADR-0003 (DB tooling)
- ADR-0005 (single-host topology)
- ADR-0006 (managed deps)
- `deployments/AGENTS.md`
- WS-03 (database & persistence core — creates the `lahijan` DB schema)
