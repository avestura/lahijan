# ADR-0047: Self-contained install (one compose file, one `.env`)

- **Status:** Accepted
- **Date:** 2026-10-03
- **Deciders:** maintainer
- **Related:** ADR-0005, ADR-0007, ADR-0040, ADR-0045, WS-23

## Context

The website advertises installing Lahijan with a single `docker compose up`, but
the real route was long: clone the repository, edit a 200-line env file with a
dozen secrets, build the dashboard with Node, and build or pull the right image.
The production compose file bind-mounts about ten repository files (Caddyfile,
database init scripts, the PowerDNS schema and template, `filer.toml`, the
migrations, `web/dist`), so it cannot run without a checkout. The page a new
reader landed on, the Quickstart, was the contributor guide (Git, Go, Node, Make
and ten free ports). The first impression was that installing Lahijan is hard.

## Decision

Add a second, self-contained deployment next to the production one:
`deployments/install/compose.yaml` plus `.env.example`. It needs nothing but
those two files.

- **No bind-mounted repository files.** The files each service needs are baked
  into small images built by CI (`lahijan-web`, `-postgres`, `-powerdns`,
  `-seaweedfs`, `-migrate`, `-init`, alongside `lahijan`). The CI `docker` job
  became a matrix over them.
- **Generated secrets.** A one-shot `secrets` service writes the passwords and
  keys to a volume on first start; an entrypoint shim in each image loads them
  without overriding anything set in `.env`. The user supplies only an admin
  email and password.
- **One public-address setting.** `LAHIJAN_DOMAIN` derives the site address, S3
  host, cookie `Secure` flag, passkey relying party, CORS origin and name server
  with compose's `${VAR:+...}${VAR:-...}` interpolation. Unset, the stack is a
  trial on `http://localhost:8080`; set, it is HTTPS on that domain.
- **Optional parts stay optional.** Compute (Incus, which needs a privileged
  container on a Linux host) is a `compute` profile; monitoring and recursive DNS
  are not part of this file (the full production compose keeps them).
- **Docs.** The Quickstart is now this install: two tabs (the real compose file
  and `.env`, included at docs build time so they cannot drift) and one command.
  Production-on-a-domain, compute and email follow as optional sections. The
  old contributor guide moved to "Run from source". The long Installation page
  stays for operators who want the full stack.
- **Docs tooling.** Tabbed code blocks (`tab="..."` fences, `include="path"` to
  pull a repository file at build time) were added to the docs compiler.

The existing production compose file, its scripts and env example are unchanged,
so current installations keep working.

## Consequences

- Two compose files to maintain. They share the service definitions' shape but
  not their text; a change to a backend's wiring has to be made in both.
  `deployments/install/README.md` documents the mapping.
- Seven images to publish instead of one. They are small and only `lahijan-web`
  (it builds the dashboard) is slow.
- The default `latest` tag is what the Quickstart uses, because that is what CI
  publishes; production guidance remains to pin a version
  (`LAHIJAN_VERSION=sha-...`).
- Generated secrets live in a Docker volume. Losing the volume makes the
  database unreadable, so the docs tell users to back it up.
- Requires Docker Compose 2.20 or newer.

## Alternatives considered

- **Inline the support files with `configs: content:`.** Makes the compose file
  about 500 lines of SQL and shell, defeating the point of a readable file, and
  needs Compose 2.23.
- **Serve the dashboard from the Go binary.** Would remove the web image but
  changes the application, and Caddy is still needed for automatic HTTPS.
- **A `curl | sh` installer.** The repository has one (`scripts/install.sh`); it
  clones, prompts and builds. It stays for the full stack, but a two-file
  install is easier to read, audit and upgrade.
- **Ask the user to generate secrets.** Several `openssl` commands and a
  pasted value per secret is exactly the friction this removes.
