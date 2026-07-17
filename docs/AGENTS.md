# AGENTS.md — Documentation (`docs/` and `docs-site/`)

> Load this **in addition to** the root `/AGENTS.md` when touching docs.

## Layout

```
docs/
  README.md                       # docs index (this folder's front door)
  adr/
    README.md                     # ADR index + status table
    0000-template.md              # copy this for any new ADR
    0001-module-path.md
    ...                           # one file per decision
  architecture/
    overview.md                   # big-picture component diagram
    conventions.md                # all coding/SQL/HTTP/frontend rules
    decisions.md                  # quick summary table of ADRs
  workstreams/
    README.md                     # workstream index with statuses
    _template.md                  # WS doc template
    WS-01-repo-tooling-hygiene.md
    ...                           # one file per workstream
  glossary.md                     # shared terms
docs-site/                        # Docusaurus 3 app
  docusaurus.config.ts            # site config
  sidebars.ts                     # sidebar layout
  package.json
  src/                            # custom React pages (landing, etc.)
  static/                         # logos, favicons, CNAME
```

## What lives where

| Content | Location | Notes |
|---------|----------|-------|
| Architectural decisions | `docs/adr/` | One file per decision, sequentially numbered. |
| Big-picture / component diagrams | `docs/architecture/` | Referenced from AGENTS.md. |
| Coding/SQL/HTTP conventions | `docs/architecture/conventions.md` | Single source of truth. |
| Workstream briefs | `docs/workstreams/` | One file per WS. Status in `README.md`. |
| Glossary | `docs/glossary.md` | Defines Tenant / User / Instance / etc. |
| User guide | `docs-site/src/` (later) | Not yet written; lands with WS-23. |
| API reference | generated from OpenAPI | Auto-injected into docs-site. |

## ADR rules

- Numbering: `NNNN-kebab-case-title.md`, zero-padded, monotonically increasing.
- Every ADR has **Status** (Proposed / Accepted / Deprecated / Superseded).
- Superseding an ADR: create a new one with `Supersedes NNNN` and flip the old
  one to `Superseded by MMMM`.
- ADRs are **immutable** once Accepted. Corrections via new ADRs, never edits.

## Workstream doc rules

- Every WS doc follows `docs/workstreams/_template.md`.
- Every WS doc has a **"Required reading"** list — the files a fresh AI session
  must load before starting work on that WS.
- Status changes (`pending → in-progress → done`) go in both the WS doc and the
  index (`docs/workstreams/README.md`).

## Docs-site rules

- Source is Docusaurus 3 (TypeScript config).
- All engineering docs in `docs/` are pulled in via the `@docusaurus/plugin-content-docs` plugin.
- Don't duplicate content. The Docusaurus site renders the markdown; it doesn't
  rewrite it.
- Use Markdown features Docusaurus supports (MDX, admonitions, tabs). Avoid raw
  HTML except where MDX is genuinely insufficient.

## Required reading before touching docs

- `/AGENTS.md` — project heart
- `docs/adr/0000-template.md` — ADR shape
- `docs/workstreams/_template.md` — WS doc shape
- ADR-0015 (REST + OpenAPI as source of truth)
- ADR-0014 (Vite SPA for both apps — affects how we document frontend)
