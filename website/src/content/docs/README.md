# Writing the Lahijan docs

Pages in this folder are compiled at build time into the documentation
section of the website (`/docs`). The sidebar and page order come from
`src/docs/nav.ts`; every `slug` there maps to `<slug>.md` here. This README
is not published.

## File format

```markdown
---
title: Instances
description: Create, start, stop, resize and delete virtual machines and system containers.
---

Opening paragraph (no heading above it).

## First section
```

- Front matter needs `title` and `description` (one sentence, ends with a
  period). Do not add a level-1 `#` heading; the title is rendered for you.
- Use `##` for sections and `###` for subsections. Both appear in the
  "On this page" list. Avoid `####` unless you really need it.
- Link to other docs pages with absolute paths without a file extension:
  `[Profiles](/docs/compute/profiles)`, `[DNSSEC](/docs/dns/dnssec#rollover)`.
  Only link to pages that exist in `nav.ts`.
- Code fences need a language. Add a file name with `title="..."`:

  ````markdown
  ```yaml title="docker-compose.prod.yml"
  services:
    lahijan:
      image: lahijan:latest
  ```
  ````

  Supported highlighting: `yaml`, `sh`, `json`, `go`, `ts`, `http`, `ini`
  (use it for `.env` files). Anything else renders as plain text.
- Commands the reader will run go in `sh` blocks (no `$ ` prompt). When the
  output matters, use the `console` language instead: prefix commands with
  `$ `, and lines without it render as output. The copy button on a
  `console` block copies the commands only.
- Callouts use GitHub alert syntax:

  ```markdown
  > [!NOTE]
  > Background the reader may want.

  > [!TIP]
  > A shortcut or good practice.

  > [!WARNING]
  > Something that can break or lose data.

  > [!CAUTION]
  > Irreversible or security-sensitive actions.
  ```

- Tables are fine for parameters, fields, options and permission lists.
- Raw HTML is disabled.

## Voice and style

- Write for a person doing a task. Lead with what the page lets them do, then
  how. Short sentences. Second person ("you"), present tense, active voice.
- Plain words. Say "use", not "leverage"; "lets you", not "empowers you to".
- **No em dashes or en dashes.** Use a period, a comma, a colon or
  parentheses instead. Hyphens in compound words are fine.
- Avoid filler and hype. Do not use: seamless, seamlessly, robust, powerful,
  cutting-edge, state-of-the-art, next-generation, leverage, empower,
  unlock, unleash, supercharge, elevate, streamline, effortless, delve,
  dive into, deep dive, harness, holistic, synergy, game-changer,
  revolutionize, journey, landscape, realm, tapestry, embark, "in today's",
  "it's worth noting", "whether you're", "look no further", "at its core".
- No emoji. No exclamation marks.
- Name UI elements exactly as the dashboard shows them, in bold:
  **Compute > New instance**.
- Use sentence case for headings ("Create a bucket", not "Create A Bucket").
- Say what happens and what can go wrong. If a feature is partial or has a
  limit, say so plainly; do not document features that do not exist.

## Naming the backends

Lahijan hides its backends from end users. In the user guides (Get started
introduction and concepts, Guides, Account, Billing, Agent, Audit log) talk
about "compute", "DNS" and "object storage", never Incus, PowerDNS or
SeaweedFS. Operator pages (Installation, Operations, Administration,
Reference > Configuration) must name them, because operators install and run
them.

## Accuracy

Every statement must match the code in this repository: the OpenAPI spec
(`api/openapi.yaml`), the handlers and services under
`internal/app/lahijan/`, the dashboard under `web/src/`, the config file
`internal/app/lahijan/conf/.lahijan.conf.default.yaml` and the compose files
under `deployments/`. When unsure, read the code; when still unsure, leave it
out.
