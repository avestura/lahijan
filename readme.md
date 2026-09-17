# Lahijan Cloud Platform

> [!WARNING]
> Work in progress and not production-ready. Bugs live here and there.

Lahijan is an open-source cloud platform that gives end users a self-service
portal for **compute, DNS, and object storage** while hiding the operator
complexity behind three best-of-breed backends:

| Capability | Backend | Lahijan's role |
|------------|---------|----------------|
| Compute (system containers + VMs) | [Incus](https://linuxcontainers.org/incus/) | Wraps the full Incus REST API; maps tenants → Incus projects. |
| DNS (authoritative zone serving) | [PowerDNS Authoritative](https://github.com/PowerDNS/pdns) | Wraps PowerDNS' HTTP API; maps tenants → zones. |
| Object storage (S3-compatible) | [SeaweedFS](https://github.com/seaweedfs/seaweedfs) | Mints per-user S3 credentials; manages buckets; users hit SeaweedFS directly for data. |

Users provision infrastructure through Lahijan's REST API or web dashboard
without ever knowing Incus/PowerDNS/SeaweedFS exist. Operators deploy a single
Docker Compose stack and Lahijan manages the rest.

## Quick start

```bash
make hooks      # install git hooks (conventional commits)
make tidy
make lint
make test
make run        # build + run with --debug
```

`make help` lists every available target.

## Where to read

- **[AGENTS.md](./AGENTS.md)** — the heart of the project. Read this first.
- **[CONTRIBUTING.md](./CONTRIBUTING.md)** — how to contribute.
- **[docs/](./docs/)** — all engineering docs:
  - **[docs/architecture/overview.md](./docs/architecture/overview.md)** — how it fits together
  - **[docs/adr/](./docs/adr/)** — every settled decision
  - **[docs/workstreams/](./docs/workstreams/)** — the 34-workstream build plan
  - **[docs/glossary.md](./docs/glossary.md)** — shared terms
- **[docs-site/](./docs-site/)** — Docusaurus app rendering the above (`make docs` to serve).

## Configuration

Lahijan is zero-config by default. Configuration is loaded in this order
(each layer overrides the previous):

1. [default configuration](./internal/app/lahijan/conf/.lahijan.conf.default.yaml) (compiled in)
2. `/etc/lahijan/.lahijan.conf.yaml`
3. `$HOME/.lahijan/.lahijan.conf.yaml`
4. `./.lahijan.conf.yaml`
5. Environment variables prefixed with `LAHIJAN_` — e.g. `LAHIJAN_HTTP_SERVER_PORT` for `http.server.port`
6. CLI arguments — e.g. `lahijan --http.server.port 5000`

CLI flags are auto-generated from the default YAML, so every key has a flag.

## License

[MIT](./LICENSE.md) © 2026 Aryan Ebrahimpour
