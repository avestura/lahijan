# ADR-0031: noVNC web console proxy — vendor assets, in-process WebSocket bridge

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** maintainer

## Context

WS-24 ("noVNC Web Console for VMs") adds a browser-based graphical console for
virtual-machine instances. The dashboard must connect a browser-side VNC
client to Incus' built-in VM VGA console without exposing the word "Incus"
or "VNC" by name to end users (pillar 1, "transparent infrastructure") and
without bypassing Lahijan's RBAC + audit layer (pillar 7).

Three sub-decisions had to be made before implementation could begin:

1. **How does the browser-side VNC client get into the dashboard bundle?**
   The official `@novnc/novnc` package is ~1 MB minified; bundling it into
   the main entry chunk would double the dashboard's gzipped first-paint
   budget (per ADR-0014 + WS-18 DoD). noVNC also ships its own ES-module
   graph that does not tree-shake cleanly when only `RFB` is needed.

2. **How does the browser reach Incus' VNC port?** Incus listens on a Unix
   socket and exposes the VM VGA console via the `/1.0/operations/<op>/websocket?
   secret=<secret>` route (the same operation-secret pattern WS-11 already
   uses for `exec`). A browser cannot speak the Incus wire protocol directly
   and cannot reach the Unix socket. Something has to bridge the browser's
   WebSocket to Incus' WebSocket.

3. **Where does the privileged action get audited?** Per pillar 7 every
   state-changing privileged action emits an audit event. Opening a VNC
   session is privileged (it grants console access to a running VM), so the
   audit event must fire even though the WS payload flows for an arbitrary
   time afterwards.

Options considered:

- **Option A — Bundle `@novnc/novnc` via npm, lazy-loaded route.** Pros:
  version-pinned, hash-verified, type-supplied. Cons: ~1 MB on the
  instance-detail chunk (WS-18's chunk budget is ~80 KB gzipped); another
  runtime+build dep to track; types are loose (the package ships its own
  `.d.ts` but they are not strict-mode-clean).
- **Option B — Vendor noVNC's pre-built static assets under
  `web/public/novnc/`, load via dynamic `<script>` at runtime.** Pros: zero
  impact on the bundle budget (assets are served as-is by Vite's static
  handler); the WS-24 doc explicitly lists `web/public/novnc/` as the
  intended location; an operator who never visits the graphical console tab
  pays nothing; the version is explicit in the README (no transitive
  resolution surprises). Cons: vendor upgrade is a manual fetch + commit
  (mitigation: README documents the exact release URL + sha256).
- **Option C — Server-side websockify-style relay with a separate process.**
  Pros: matches the upstream `novnc + websockify` reference deployment.
  Cons: introduces a new process to supervise (violates pillar 8
  "monolithic Go backend"); duplicates session/auth context across a
  process boundary; offers no benefit over an in-process bridge because
  Lahijan already owns the auth + audit + Incus driver surface.
- **Option D — Browser speaks Incus wire protocol directly.** Not viable:
  Incus is on a Unix socket and pillar 1 forbids exposing the Incus name.

## Decision

WS-24 implements the noVNC web console as follows:

1. **Frontend asset strategy: Option B.** The dashboard vendors noVNC's
   pre-built `core/` JavaScript bundle under `web/public/novnc/`. The
   `InstanceGraphicalConsole` React component dynamically injects
   `<script src="/novnc/core/rfb.js">` the first time the user opens the
   "Console (Graphical)" tab, then constructs `window.RFB` against the
   Lahijan WebSocket URL. If the assets are absent (operator did not run
   the vendor step) the component renders a localised "noVNC not vendored"
   notice instead of crashing. A README at `web/public/novnc/README.md`
   documents the exact upstream release, sha256, and re-vendor command.

2. **Backend bridge: in-process WebSocket proxy.** A new WebSocket route
   `GET /api/v1/compute/instances/{instanceId}/vnc` (registered manually,
   not via `apigen`, because oapi-codegen cannot model WebSocket upgrades)
   upgrades the request, validates the instance is a VM + running, opens
   the Incus console operation via `Provider.OpenVNCConsole`, dials the
   Incus operation's per-secret WebSocket via
   `Provider.DialVNCConsole`, and pumps bytes both directions until either
   side closes. The route is registered AFTER `apigen.RegisterHandlers` so
   it does not conflict with the generated routing table.

3. **Privileged action: new permission + audit event.** WS-24 introduces a
   new permission `compute.instance.console.vnc` (distinct from
   `compute.instance.exec` per the WS-24 scope) granted to `tenant.admin`
   and `tenant.member` (not `tenant.viewer`). The audit gate dispatches
   the WS path to `RequirePerm(PermComputeInstanceConsoleVNC)`. The
   handler emits an audit row with action
   `compute.instance.console.vnc.connect` (status=success) before the
   bytes flow and marks the outcome when the bridge closes. No MarkOutcome
   to failure on disconnect — a clean WebSocket close is a success.

4. **Frontend gating.** The "Console (Graphical)" tab is rendered only when
   `inst.type === "virtual-machine"` AND `usePerm("compute.instance.console.vnc")`
   returns true. Containers do not get the tab (they only get the xterm.js
   console from WS-14).

This ADR narrows ADR-0025's note that the WS-11 exec websocket pattern is
"incompatible with WS-24's noVNC": the *protocol* is incompatible (exec is
3-fd; VNC is a single binary stream), but the *operation-secret bootstrap*
is reused. `Provider.OpenVNCConsole` and `Provider.DialVNCConsole` share
the `execOpen` + `execDial` plumbing already present in `exec.go`.

## Consequences

- **Positive:** the dashboard bundle budget is untouched — noVNC only
  downloads when the user actually opens the graphical console tab.
- **Positive:** the audit trail captures every VNC session-open as a
  distinct, queryable event (`action = compute.instance.console.vnc.connect`).
- **Positive:** the bridge is pure Go in-process — no new process to
  supervise, no port to expose, no extra container in the compose stack.
- **Positive:** the WS-24 scope's "noVNC static assets served from the
  dashboard (`web/public/novnc/`)" item is satisfied verbatim.
- **Negative:** vendor upgrades are manual (fetch release tarball, copy
  `core/` into `web/public/novnc/`, update sha256 in README). Mitigation:
  README documents the exact command + sha256 verification step.
- **Negative:** the WS endpoint is not in `api/openapi.yaml` as a normal
  operation (it has no `operationId`) so oapi-codegen does not generate a
  typed handler stub. Mitigation: the path is documented in the spec with
  a description + the standard error envelopes so the generated TS client
  type still recognises it; the handler is hand-written in
  `internal/app/lahijan/api/compute_vnc_handlers.go`.
- **Neutral:** session-cookie auth on WebSocket works for same-origin
  deployments (dashboard + API behind the same Caddy reverse proxy per
  ADR-0030). A future WS that splits the dashboard onto a different origin
  would need to mint a short-lived WS token; that's out of scope here.

## Compliance

- `web/public/novnc/README.md` documents the vendored noVNC release + sha256.
- `web/public/novnc/` contains the upstream `core/` assets when the
  operator has run the vendor step.
- `internal/app/lahijan/providers/incus/console.go` defines
  `OpenVNCConsole` + `DialVNCConsole`; both open OTel spans via the
  package-local tracer (per ADR-0016).
- `internal/app/lahijan/providers/incus/fake/console.go` extends the
  httptest fake to cover the console endpoint so unit tests do not need
  a real Incus daemon (per ADR-0025 + ADR-0029).
- `internal/app/lahijan/api/compute_vnc_handlers.go` is the WebSocket
  upgrade handler; the audit gate in `api/router.go` dispatches the path
  to `RequirePerm(rbac.PermComputeInstanceConsoleVNC)`.
- `internal/app/lahijan/auth/rbac/permissions.go` declares
  `PermComputeInstanceConsoleVNC`; `auth/rbac/roles.go` grants it to
  `tenant.admin` and `tenant.member`.
- `web/src/features/compute/components/InstanceGraphicalConsole.tsx`
  dynamically loads `/novnc/core/rfb.js` and gates rendering on
  `usePerm("compute.instance.console.vnc")`.
- The OpenAPI spec (`api/openapi.yaml`) documents the WS path
  `/api/v1/compute/instances/{instanceId}/vnc` without an `operationId`
  so oapi-codegen does not emit a handler stub.

## References

- WS-24 (noVNC Web Console for VMs)
- ADR-0010 (full Incus surface — VNC console is part of it)
- ADR-0025 (Incus client library choice — operation-secret pattern reused)
- ADR-0014 (Vite SPA — bundle-budget rationale for static-asset vendoring)
- ADR-0030 (production deployment topology — single-host Caddy reverse proxy)
- [noVNC docs](https://novnc.com/info.html)
- [Incus VM console access](https://linuxcontainers.org/incus/docs/main/howto/instances_console/)
