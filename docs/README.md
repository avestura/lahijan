# Lahijan Documentation

This folder holds all engineering documentation for Lahijan: architecture,
conventions, decisions (ADRs), workstreams, and the glossary.

The markdown here is rendered into a docs website by [Docusaurus 3](https://docusaurus.io/),
whose app lives in [`../docs-site/`](../docs-site/). Run `make docs` from the
repo root to serve the site locally.

## What's in here

| Folder | Purpose |
|--------|---------|
| [`adr/`](./adr/) | Architecture Decision Records — one per settled decision. Start at [`adr/README.md`](./adr/README.md). |
| [`architecture/`](./architecture/) | Big-picture docs: [`overview.md`](./architecture/overview.md), [`conventions.md`](./architecture/conventions.md), [`decisions.md`](./architecture/decisions.md). |
| [`workstreams/`](./workstreams/) | All workstream briefs (WS-01..WS-30). Start at [`workstreams/README.md`](./workstreams/README.md). |
| [`glossary.md`](./glossary.md) | Shared terms (Tenant, User, Instance, Zone, Bucket, Plugin, ...). |
| [`AGENTS.md`](./AGENTS.md) | Instructions for AI sessions working in `docs/`. |

## Where to start (for humans)

1. **What is Lahijan?** — [`/AGENTS.md`](../AGENTS.md) (top section).
2. **How does it fit together?** — [`architecture/overview.md`](./architecture/overview.md).
3. **What's been decided?** — [`architecture/decisions.md`](./architecture/decisions.md) (one-line each) or full ADRs in [`adr/`](./adr/).
4. **What's the plan?** — [`workstreams/README.md`](./workstreams/README.md) (status index).
5. **How do I contribute?** — [`/CONTRIBUTING.md`](../CONTRIBUTING.md).

## Where to start (for AI sessions)

1. [`/AGENTS.md`](../AGENTS.md) — read end to end.
2. The relevant workstream doc in [`workstreams/`](./workstreams/).
3. That workstream's **"Required reading"** list.
4. [`architecture/conventions.md`](./architecture/conventions.md) for the area.
5. The relevant skill in [`.opencode/skills/`](../.opencode/skills/).

The full workflow is enforced by the `ws-implementer` subagent
([`.opencode/agent/ws-implementer.md`](../.opencode/agent/ws-implementer.md)).

## Editing docs

- Use Markdown features Docusaurus supports (MDX, admonitions, tabs).
- Don't duplicate content — if it lives in an ADR, link instead of re-stating.
- ADRs are immutable once Accepted; corrections land as new ADRs.
- Workstream docs change Status as work progresses.
