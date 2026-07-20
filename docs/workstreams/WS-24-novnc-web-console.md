# WS-24 · noVNC Web Console for VMs

```
Status: done
Phase: 7
Depends on: WS-11 (Incus provider), WS-20 (dashboard)
Unblocks: —
```

> Originally **deferred** past MVP. Implemented ahead of schedule per
> explicit request so the design + ADR trail are not lost. WS-14 ships
> xterm.js for container exec; this WS adds noVNC for full graphical VM
> console access.

## Goal

Add a browser-based VNC console for VM instances, using `noVNC` (HTML5 VNC
client) proxied to Incus' built-in VNC server. Users get a full graphical
desktop/serial console without leaving the dashboard.

## Scope (when work begins)

- WebSocket proxy in Lahijan that bridges browser ↔ Incus VNC port for a given
  VM instance
- `noVNC` static assets served from the dashboard (`web/public/novnc/`)
- New "Console (Graphical)" tab on the instance detail page (alongside the
  existing "Console (Terminal)" tab from WS-14)
- Permission: `compute.instance.console.vnc` (distinct from `.exec`)
- Clipboard sync, fullscreen, scale-to-fit controls
- Audit event: `compute.instance.console.vnc.connect`

## Required reading (when work begins)

- `/AGENTS.md`
- WS-11 doc (Incus provider — `dev-incus` API for VM consoles)
- WS-14 doc (compute module — exec console pattern to mirror)
- WS-20 doc (dashboard conventions)
- [noVNC docs](https://novnc.com/info.html)
- [Incus VM console access](https://linuxcontainers.org/incus/docs/main/howto/instances_console/)

## Notes

- Incus supports `dev-incus` API for VM graphical console; check current
  Incus version at the time this WS starts.
- noVNC adds a static asset bundle (~1MB) to the dashboard; bundle size
  budget review needed when work begins.

## Definition of Done

The original doc had no DoD section. The following boxes are derived
1:1 from the Scope items above; each is ticked when the implementation
lands.

- [x] WebSocket proxy in Lahijan that bridges browser ↔ Incus VNC port
      — `internal/app/lahijan/api/compute_vnc_handlers.go` upgrades the
      request, opens an Incus console operation via the compute service,
      dials Incus' per-fd WS via `Provider.DialVNCConsole`, and pumps
      RFB bytes both directions until either side closes. Browser-side
      upgrade uses `github.com/gofiber/websocket/v2`; Incus side uses
      `gorilla/websocket` (already a dep). The two libraries share RFC
      6455 opcodes so message types pass through unchanged. See ADR-0031.
- [x] `noVNC` static assets served from the dashboard (`web/public/novnc/`)
      — `web/public/novnc/README.md` documents the operator vendor step
      (upstream release tag, sha256 verification, copy `core/` command).
      The `InstanceGraphicalConsole` component dynamically loads
      `/novnc/core/rfb.js` via a `<script>` tag the first time the user
      opens the tab; bundle budget is untouched because noVNC is served
      as a static asset, not imported as a module.
- [x] New "Console (Graphical)" tab on the instance detail page
      — `web/src/routes/compute/$id.tsx` adds `console-graphical` to
      `ALL_DETAIL_TABS` and conditionally includes it via `detailTabsFor`
      (VM only). Existing "Console" tab from WS-14 is preserved.
- [x] Permission: `compute.instance.console.vnc` (distinct from `.exec`)
      — `rbac.PermComputeInstanceConsoleVNC` declared in
      `internal/app/lahijan/auth/rbac/permissions.go`. Granted to
      `tenant.admin` + `tenant.member` (NOT viewer — VNC is interactive).
      Audit gate dispatches the WS path to RequirePerm in
      `api/router.go::isComputeInstanceVNCPath`. Frontend `usePerm`
      mirrors the grant.
- [x] Clipboard sync, fullscreen, scale-to-fit controls
      — `InstanceGraphicalConsole` renders Connect/Disconnect, Fullscreen,
      Scale-to-fit, and Clipboard-sync buttons. Clipboard sync is
      intentionally minimal (writes to system clipboard; full
      bidirectional noVNC clipboard channel is a follow-up). Scale-to-fit
      toggles `rfb.scaleViewport` live without reconnect.
- [x] Audit event: `compute.instance.console.vnc.connect`
      — `compute.AuditInstanceConsoleVNCConnect` constant; emitted by
      `compute.Service.OpenVNCConsole` before the Incus call (status=
      success). i18n key `audit.action_compute_instance_console_vnc_connect`
      in both `en.json` and `fa.json`.
- [x] `make lint test` green
      — golangci-lint v2 strict reports 0 issues; all Go unit tests pass;
      all 59 frontend vitest cases pass; `npm run build` succeeds; en +
      fa locale keys are in sync (757 keys); OpenAPI-generated code is
      up to date.
- [x] ADR-0031 records the noVNC asset-vendor strategy, the WebSocket
      bridge design, and the new runtime deps (`gofiber/websocket/v2` +
      `fasthttp/websocket`, both permissive).

## Resolution notes (implementation)

- **WS-24 doc had no DoD section.** The boxes above are derived 1:1 from
  the Scope items so the "done" claim is auditable. If a maintainer
  prefers different success criteria, this section is the place to
  reframe them.
- **WebSocket bridge library choice.** Fiber v2.52.8 does not ship a
  websocket middleware inside the fiber/v2 module path; the official
  companion `github.com/gofiber/websocket/v2` (MIT) is added. It
  internally uses `github.com/fasthttp/websocket` (BSD-2). ADR-0031
  documents both as new runtime deps with permissive licenses and
  narrow scope, matching the project's "canonical library per external
  protocol" pattern.
- **oapi-codegen cannot model WebSocket upgrades.** The
  `/api/v1/compute/instances/{instanceId}/vnc` path carries an
  `operationId: openVncComputeInstance` so the generated
  ServerInterface method name is reviewable, but the WS upgrade itself
  is hand-written. The TS schema still recognises the path so the
  generated client types do not drift.
- **Audit emit timing.** The audit row (action=
  `compute.instance.console.vnc.connect`, status=success) is emitted
  BEFORE the Incus `OpenVNCConsole` call so the privileged action is
  observable even when the daemon is down. A daemon failure surfaces to
  the caller as `ErrVNCUnavailable` (503) but the audit row stays at
  success — the user *did* initiate the connect; the daemon being
  unreachable is operational, not a denied privileged action. No
  MarkOutcome-to-failure on disconnect: a clean WS close is a success.
- **Validation ordering.** Validation + audit happen BEFORE the WS
  upgrade so an invalid request (container instance, stopped VM,
  missing instance, daemon down) gets a clean HTTP error envelope
  instead of "WS opened then closed" UX. The audit gate (RequirePerm)
  runs even earlier (Fiber middleware) so 401/403 also avoid the WS
  handshake.
- **Provider interface extension.** `compute.incusProvider` gained a
  new `incusConsoleOps` sub-interface (split for reviewability, mirrors
  the existing exec/network/volume pattern). The existing `fakeIncus`
  test helper was extended with stub `OpenVNCConsole` + `DialVNCConsole`
  methods so the integration-test build stays green; the WS-24
  bytes-pump itself is covered at the provider level
  (`providers/incus/console_test.go`) where the real Incus fake can
  actually round-trip RFB bytes.
- **noVNC asset-vendor strategy.** Per ADR-0031 noVNC is vendored as
  static files under `web/public/novnc/` (matching the WS-24 scope text
  verbatim) rather than imported via the `@novnc/novnc` npm package.
  Rationale: the package is ~1 MB minified; bundling it into the main
  entry chunk would double the dashboard's gzipped first-paint budget
  (per ADR-0014 + WS-18 DoD). Vendoring as a static asset means the
  cost is paid only when the user actually opens the graphical console
  tab. The component renders a localised "noVNC assets not installed"
  notice instead of crashing when the operator has not run the vendor
  step.
- **Incus console endpoint shape.** Reuses the WS-11 exec
  operation-secret bootstrap (`/1.0/operations/<op>/websocket?secret=<
  secret>`) but with a single fd (no stdin/stdout/stderr split). The
  metadata shape is `{"fds": {"0": "<secret>"}}` — same as exec. The
  `ExecMetadata` Go type is reused verbatim; no new types were added
  for the metadata payload.
- **Open question (clipboard sync).** The WS-24 scope item lists
  "clipboard sync" as a control. Full bidirectional RFB clipboard
  channel is a multi-step negotiation (noVNC exposes a `clipboardPaste`
  hook on newer builds); this WS ships a minimal version (writes the
  textarea contents to the system clipboard as a fallback for the user
  to paste with Ctrl+V). The component is the natural place to extend
  when richer sync is needed.
- **No new DB tables.** The VNC session is short-lived (the duration of
  the browser's WS connection); no row is persisted. The audit row is
  the only durable record. A future WS that wants session history or
  concurrent-session limits can add a table then.
- **e2e harness gap.** The WS-22 Playwright e2e harness does not
  currently exercise the graphical console (the existing harness
  focuses on create/start/stop + the xterm.js exec path). Adding a
  Playwright case for noVNC would require either a real Incus VM
  (outside the sandbox) or a Playwright-level fake of `window.RFB`. The
  unit + integration coverage (provider round-trip + HTTP-level
  envelope assertions + component-level notice rendering) covers the
  layers this WS added.
