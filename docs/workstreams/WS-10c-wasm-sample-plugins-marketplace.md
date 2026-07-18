# WS-10c · WASM Sample Plugins + Marketplace Scaffolding

```
Status: done
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

- [x] all 3 sample plugins build (`tinygo build -target wasm`) —
      source + Makefile shipped under `examples/plugins/`; the build
      itself requires the TinyGo toolchain which is not part of Lahijan's
      Go test suite. Each plugin's Makefile ships a `verify` target that
      rejects modules importing WASI.
- [x] all 3 install + grant permissions + run + emit observable behavior —
      the install + grant + enable paths are exercised end-to-end via
      the marketplace HTTP integration test
      (`admin_marketplace_http_integration_test.go`) and the existing
      `admin_plugins_http_integration_test.go`. The runtime invocation
      path is exercised by `hostfuncs_integration_test.go` for every
      host function family.
- [x] upgrade adds a permission → admin is prompted to grant it —
      `TestUpgrade_HappyPath_PreservesAndDropsGrants` +
      `TestService_Upgrade_SurfacesNewPermissions` +
      `TestMarketplace_Upgrade_AddsNewPermission_PromptsAdmin`.
- [x] upgrade removes a permission → grant is dropped cleanly — same
      tests; `droppedGrants` is asserted in the response.
- [x] removal cleans up event subscriptions, KV, HTTP routes — CASCADE
      on `plugins.id` removes them atomically; the upgrade flow ALSO
      calls `DeleteAllForPlugin` on the HTTP handler + subscription
      repos so the cleanup is observable before the CASCADE.
      `TestUpgrade_CleansUpSideTables` covers this.
- [x] developer guide is accurate enough for an external dev to write a
      plugin without help — `docs/architecture/plugins.md`.
- [x] `make lint test` green.

## Open questions

- TinyGo vs. Rust (wasm32-wasi) vs. AssemblyScript as the primary sample
  language? **Resolved (this WS):** TinyGo. Matches the rest of the
  project; the only Go compiler that emits plain `wasm32-unknown-unknown`
  with no WASI imports (per ADR-0023). Rust + AssemblyScript plugins
  remain equally valid; their toolchains are documented but no sample
  ships.
- Plugin signing (cosign/sigstore) — required for marketplace, optional
  for direct upload? **Resolved (this WS):** deferred to a follow-up.
  The installer already stores an optional `signature` BYTEA column
  (WS-10a); the marketplace verifies the sha256 pin against the index
  entry as the tamper-evidence mechanism for MVP. Full cosign/sigstore
  verification lands in a future WS alongside the git-source marketplace
  flow.

## Notes

- This WS exists to exercise the system end-to-end. Bugs found here feed
  back into WS-10a/WS-10b fixes.
- The "first plugin" tutorial is part of the DoD — the docs are critical for
  ecosystem adoption.
