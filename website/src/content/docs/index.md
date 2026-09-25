---
title: Lahijan documentation
description: Guides and reference for using, administering, installing and extending Lahijan.
---

Lahijan is an open-source cloud platform you run on your own servers. It gives the people who use it a self-service dashboard and a REST API for compute instances (system containers and virtual machines), authoritative DNS zones and S3-compatible object storage. Around those three services it adds the parts a shared platform needs: tenants and roles, an append-only audit log, a prepaid billing ledger, sign-in security and a sandboxed plugin system.

These docs are written for four groups of readers:

- **People using a Lahijan deployment**, who create instances, zones and buckets from the dashboard or the API. Start with the Guides section.
- **Administrators**, who top up balances, manage plugins and set policy for a deployment. See the Administration section.
- **Operators**, who install Lahijan on a server, keep it running and upgrade it. See Install on a server and the Operations section.
- **Plugin authors**, who extend Lahijan with WebAssembly plugins. See the Plugins section.

> [!WARNING]
> Lahijan is a young project and not yet production-ready. Expect rough edges, and test backups and upgrades before you rely on them.

## Where to start

- [Introduction](/docs/getting-started/introduction): what Lahijan offers and how it is used.
- [Core concepts](/docs/getting-started/concepts): tenants, roles, the audit log, billing and plugins.
- [Quickstart](/docs/getting-started/quickstart): run Lahijan on your own machine to try it or work on it.
- [Install on a server](/docs/getting-started/installation): set up the production stack on a Linux host.
- [First steps](/docs/getting-started/first-steps): sign in for the first time and create your first resources.
- [REST API](/docs/reference/api): call Lahijan from scripts and other programs.
- [Glossary](/docs/reference/glossary): the terms used throughout these docs.
