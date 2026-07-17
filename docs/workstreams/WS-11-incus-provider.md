# WS-11 · Incus Provider

```
Status: pending
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

- [ ] `Ping(ctx)` works against a real Incus socket
- [ ] every Incus API surface (instances, images, profiles, devices, networks,
      projects, storage) is reachable through the driver
- [ ] tenant → project mapping tested
- [ ] restricted project defaults enforced (a tenant can't see another's
      instances through Incus)
- [ ] events from Incus propagate to the event bus
- [ ] `exec` works (round-trips a command via websocket in integration test)
- [ ] all provider calls are traced (OTel spans)
- [ ] `make lint test` green

## Open questions

- Library choice: `github.com/lxc/incus/client` (official). License: Apache-2.
  (Default: use it.)
- For dev without a real Incus daemon: do we ship a fake in compose, or do
  tests stub it? (Default: tests stub via httptest; the real daemon is only
  in integration / e2e.)
- Image catalog: which images to seed as "featured"? (Default: ubuntu/24.04,
  debian/12, alpine/3.20, fedora/40 — matches our kickoff assumptions.)

## Notes

- This WS defines the `PlacementDriver` interface even though the MVP impl is
  `LocalPlacementDriver`. WS-26 swaps in the cluster version.
- The `exec` websocket implementation is reused by WS-14 (xterm.js console).
