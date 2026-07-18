# WS-11 · Incus Provider

```
Status: done
Phase: 3
Depends on: WS-05
Unblocks: WS-14 (compute module), WS-24 (noVNC), WS-25 (snapshots), WS-30 (public IPs)
```

## Goal

Build the driver that translates Lahijan's domain into Incus API calls. After
this WS, the compute module (WS-14) can call into a typed Go client over the
full Incus surface: instances, images, profiles, devices, networks, projects,
storage pools, events, exec.

## Scope

**In scope:**
- `internal/app/lahijan/providers/incus/`:
  - `client.go` — wraps the official `incus-client` Go SDK; configured via
    `conf.providers.incus.*`
  - `projects.go` — create/list/delete Incus projects; tenant → project mapping
    (`lahijan-tenant-<uuid>`)
  - `instances.go` — full instance lifecycle (create from image, start, stop,
    restart, freeze, pause, delete, migrate)
  - `images.go` — list public + private images, copy, import from alias
  - `profiles.go` — list/create/update/delete profiles within a project
  - `devices.go` — attach/detach devices (nic, disk, gpu, proxy, ...)
  - `networks.go` — project-scoped networks, ACLs, forwards, zones
  - `storage.go` — storage pools, volumes
  - `events.go` — websocket event listener (instance state changes, etc.) →
    emits events into the WASM event bus + audit log
  - `exec.go` — websocket-based `incus exec` proxy for the future xterm.js
    console (WS-14 uses this; WS-24 adds noVNC)
- Tenant bootstrap hook: when a tenant is created, its Incus project is
  created with restricted defaults (limited device types, network access, etc.)
- Health probe: `Ping(ctx)` — used by `Provider` interface.
- Capabilities: advertised via `Capabilities()` (e.g. "cluster mode", "VM
  support").
- Compose: `incus-client` container with the host socket mounted; Lahijan
  connects via Unix socket.

**Out of scope:**
- The user-facing Compute module (WS-14) — this WS is purely the driver.
- Multi-node scheduling (the `PlacementDriver` is a single local node here;
  WS-26 swaps it out).
- noVNC console for VMs (WS-24).
- Snapshot scheduling (WS-25).

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0010-full-incus-surface.md`
- `docs/adr/0005-mvp-topology.md` (Incus on local socket for MVP)
- `docs/adr/0009-multi-node-ready.md` (the PlacementDriver abstraction)
- WS-05 doc (Provider interface lives here)
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- Full Incus driver implementing the Provider interface + every area method
- Tenant project bootstrapping with restricted defaults
- Event listener that fans Incus events into the WASM event bus + audit log
- Compose entry for `incus-client`
- Integration tests using the fake Incus REST server (built in WS-22, stubbed
  here via httptest)

## Definition of Done

- [x] `Ping(ctx)` works against a real Incus socket
- [x] every Incus API surface (instances, images, profiles, devices, networks,
      projects, storage) is reachable through the driver
- [x] tenant → project mapping tested
- [x] restricted project defaults enforced (a tenant can't see another's
      instances through Incus)
- [x] events from Incus propagate to the event bus
- [x] `exec` works (round-trips a command via websocket in integration test)
- [x] all provider calls are traced (OTel spans)
- [x] `make lint test` green

## Resolution notes (implementation)

- **Library choice (resolved via ADR-0025):** the WS doc lists
  `github.com/lxc/incus/client` (official) as the default, but the WS-11
  implementation ships a thin internal REST client over the Incus REST API
  instead. The official SDK is a heavyweight dependency (large transitive
  tree) and its connection bootstrap makes httptest fakes painful. The
  internal client adds exactly one runtime dependency
  (`github.com/gorilla/websocket`, BSD-2-Clause) for the events + exec
  websockets; the rest is plain `net/http`. ADR-0025 documents the
  decision and narrows ADR-0010's compliance bullet from "wraps the Incus
  Go SDK" to "wraps the Incus REST API from Go."
- **Fake Incus server (resolved per WS doc default):** tests stub via
  httptest (`internal/app/lahijan/providers/incus/fake/server.go`). The
  fake implements every endpoint the driver touches, plus the events +
  exec websockets. The same fake will be reused by WS-14 (compute module)
  unit tests; the WS-22 sandbox integration harness may swap it for a
  richer fake if it turns out to need more coverage.
- **Image catalog (resolved per WS doc default):** the four default
  featured images (ubuntu/24.04, debian/12, alpine/3.20, fedora/40) are
  seeded via `conf.providers.incus.featuredImages` and surfaced via
  `incus.FeaturedImages(cfg)`. WS-14 renders them in the image picker.
- **PlacementDriver abstraction (added per the WS doc notes):** the driver
  exposes the per-instance lifecycle (CreateInstance, SetInstanceState,
  ...) which a future `PlacementDriver` wrapper can layer scheduling on
  top of. The MVP `LocalPlacementDriver` is implicit (the driver points
  at one daemon); WS-26 swaps it for a `ClusterPlacementDriver` without
  touching the compute module.
- **Exec websocket protocol:** the implementation opens three websockets
  per exec (stdin/stdout/stderr) using the per-fd secrets the daemon
  returns in the operation metadata. The bytes are pumped in goroutines;
  the operation is waited on for the exit code. The same pattern will be
  reused by WS-14's xterm.js console (interactive, not captured) and is
  incompatible with WS-24's noVNC (different protocol).
- **Event listener lifecycle:** `StartEventListener` returns an
  `*EventListener` whose `Close()` is wired into `program.Start` via
  `defer`. The listener uses exponential backoff with jitter on
  transient disconnects; permanent dial errors (auth failure, etc.)
  abort the loop and log at Error.

## Notes

- This WS defines the `PlacementDriver` interface even though the MVP impl is
  `LocalPlacementDriver`. WS-26 swaps in the cluster version.
- The `exec` websocket implementation is reused by WS-14 (xterm.js console).
