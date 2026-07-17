# WS-10c · WASM Sample Plugins + Marketplace Scaffolding

```
Status: pending
Phase: 2
Depends on WS-10b
Unblocks: —
```

## Goal

Prove the plugin system works end-to-end with real, useful plugins, and stand
up the marketplace scaffolding so plugins can be browsed, installed, and
updated. After this WS, third-party developers have a clear template to
follow.

## Scope

**In scope:**

### Sample plugins (in `examples/plugins/`)
- **`slack-notifier`** (TinyGo, ~50 LOC) — listens to `compute.instance.*`
  events; sends a Slack webhook. Declares `events.listen:compute.instance.*`
  and `network.outbound:hooks.slack.com`.
- **`autoscaler-stub`** (TinyGo) — listens to `compute.instance.cpu_high`;
  emits a comment in the audit log; grounds a future real autoscaler. Uses
  `events.listen:compute.instance.cpu_high`, `kv.read:metrics`,
  `kv.write:metrics`.
- **`dns-record-hook`** (TinyGo) — listens to `dns.record.created`; calls an
  external webhook. Uses `events.listen:dns.record.*`,
  `network.outbound:example.com`.
- A **plugin template** (`examples/plugins/_template/`) with manifest,
  Makefile, and a hello-world main.go that compiles to WASM via TinyGo.

### Marketplace scaffolding
- `internal/app/lahijan/plugins/marketplace/`:
  - marketplace index format (`plugins-marketplace.yaml`) — a Git URL or
    directory serving a list of plugins
  - `POST /api/v1/admin/plugins/install/{name}` — fetches from the
    marketplace, runs the normal install flow
  - `GET /api/v1/admin/marketplace` — list available plugins
- A minimal default marketplace (a Git repo we host later, or a directory in
  this repo serving as the index)
- Plugin **upgrade** flow: install v1 → marketplace has v2 → upgrade
  preserves grants (or prompts re-approval if new permissions are requested)
- Plugin **removal** flow: disable + remove + audit + cleanup

### Documentation
- `docs/architecture/plugins.md` — full developer guide:
  - the manifest schema
  - the host function reference (from WS-10b)
  - the build process (TinyGo → WASM)
  - the install flow
  - permission reference
- A "write your first plugin" tutorial

**Out of scope:**
- A public hosted marketplace (this WS ships scaffolding; the actual hosting
  is operator infrastructure).
- Paid plugins / licensing (Phase 7).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0012-wasm-full-mvp.md`
- WS-10a, WS-10b docs (the runtime this WS exercises)
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- 3 working sample plugins
- Plugin template repo-in-repo
- Marketplace install flow
- Plugin upgrade flow (with re-grant prompt for new permissions)
- Developer guide doc

## Definition of Done

- [ ] all 3 sample plugins build (`tinygo build -target wasm`)
- [ ] all 3 install + grant permissions + run + emit observable behavior
- [ ] upgrade adds a permission → admin is prompted to grant it
- [ ] upgrade removes a permission → grant is dropped cleanly
- [ ] removal cleans up event subscriptions, KV, HTTP routes
- [ ] developer guide is accurate enough for an external dev to write a
      plugin without help
- [ ] `make lint test` green

## Open questions

- TinyGo vs. Rust (wasm32-wasi) vs. AssemblyScript as the primary sample
  language? (Default: TinyGo — matches the rest of the project.)
- Plugin signing (cosign/sigstore) — required for marketplace, optional for
  direct upload? (Default: optional for direct upload; required for
  marketplace.)

## Notes

- This WS exists to exercise the system end-to-end. Bugs found here feed
  back into WS-10a/WS-10b fixes.
- The "first plugin" tutorial is part of the DoD — the docs are critical for
  ecosystem adoption.
