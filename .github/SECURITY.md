# Security Policy

## Supported versions

Lahijan is pre-1.0. Only the latest `main` branch and the most recent release
are eligible for security fixes.

| Version | Supported          |
|---------|--------------------|
| main    | :white_check_mark: |
| < 1.0   | :white_check_mark: (latest tag only) |

## Reporting a vulnerability

**Please do NOT open a public GitHub issue for security vulnerabilities.**

Instead, email the maintainer at **security@avestura.dev** with:

1. A description of the issue and its potential impact.
2. Steps to reproduce, or a proof-of-concept.
3. Affected versions / commits.
4. Any suggested mitigations.

You should receive a response within **72 hours**. If you don't, please follow
up. We will coordinate disclosure timing with you and credit reporters in the
advisory unless you prefer to remain anonymous.

## Hardening recommendations for self-hosters

When deploying Lahijan, pay special attention to:

- **The Incus socket** — Lahijan controls Incus over its Unix socket. Whoever
  can reach that socket has root-equivalent power over the host. Run Lahijan as
  a dedicated unprivileged user that is the only member of the `incus-admin`
  group.
- **PowerDNS API key** — PowerDNS' HTTP API accepts a static API key. Store it
  in an environment variable, not in a checked-in config file.
- **SeaweedFS S3 admin credentials** — rotate regularly; Lahijan will mint
  per-user sub-credentials rather than handing out the admin pair.
- **PostgreSQL DSN** — use SSL/TLS (`sslmode=require`) in production.
- **Audit log table** — once written, rows must be append-only. Enforce with
  a database trigger (added in WS-08). Consider replicating to cold storage.

See `docs/architecture/conventions.md#security` for the full hardening checklist.
