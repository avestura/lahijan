# AGENTS.md — Deployments (`deployments/`)

> Load this **in addition to** the root `/AGENTS.md` when touching compose or
> install scripts.

## Files (target)

```
deployments/
  docker-compose.dev.yml          # dev: Postgres + Incus-client + PDNS + SeaweedFS + OTel collector
  docker-compose.prod.yml         # prod: same + the Lahijan image, TLS, persistent volumes
  docker-compose.test.yml         # CI/integration: same as dev but isolated network, throwaway volumes
  .env.example                    # template; copy to .env and fill in
  incus/
    preseed.yaml                  # `incus admin init --auto` config applied at first boot
  powerdns/
    pdns.conf.template            # templated at startup with env values
  seaweedfs/
    filer.toml.template           # Postgres-backed filer config
  otel/
    otel-collector-config.yaml    # collector → Jaeger + Loki + Prometheus
  README.md                       # operator guide (links to docs-site)
```

## Compose rules

- **One stack, one command.** `docker compose -f deployments/docker-compose.prod.yml up -d`
  must bring up the whole platform. No manual pre-steps beyond seeding `.env`.
- **Dev mirrors prod.** `docker-compose.dev.yml` uses the same images and the
  same topology as prod; only ports, replicas, and TLS differ.
- **Single Postgres, multiple logical DBs.** `lahijan`, `pdns`, `seaweed`, and
  River's `river_*` schema all live in one Postgres container in dev. In prod
  they MAY be split, but the default compose stack keeps them together (see
  ADR-0007).
- **Secrets via env, never committed.** Every secret is read from `${VAR}`;
  `.env.example` lists the names but never values.
- **Volumes for state.** Postgres data, SeaweedFS volumes, PowerDNS zone
  storage, and Lahijan's plugin upload dir are all on named volumes so
  `docker compose down` doesn't lose data.

## Provider bring-up

- **Incus** runs on the host (not in a container) because it needs kernel
  access. The compose stack ships `incus-client` as a sidecar with the socket
  mounted; Lahijan talks to it over the Unix socket.
- **PowerDNS Authoritative** runs in a container; its gpgsql backend points at
  the shared Postgres with `DATABASE_NAME=pdns`. The API key is set from env.
- **SeaweedFS** runs in `weed mini` mode for dev (single process) and split
  (master + volume + filer + s3) for prod. Filer store is Postgres
  (`DATABASE_NAME=seaweed`).
- **OTel collector** ingests OTLP from Lahijan and fans out to Jaeger (traces),
  Loki (logs), and Prometheus (metrics) — all in the same stack.

## Required reading before touching deployments

- `/AGENTS.md` — project heart
- ADR-0005 (single-host topology, multi-host-ready)
- ADR-0006 (managed deps: Lahijan ships the whole stack)
- ADR-0007 (shared Postgres, separate logical DBs)
- `docs/workstreams/WS-23-production-deployment-ops.md`
- The relevant provider WS (WS-11/12/13) — they document their container needs

## Hard rules

- Never commit `.env`, only `.env.example`.
- Never disable TLS in `docker-compose.prod.yml`.
- Never expose the Incus socket to the network; it's Unix-only.
- Never use `latest` tags in prod compose; pin to a SemVer.
- Every new container must ship a `HEALTHCHECK` so compose can detect failures.
