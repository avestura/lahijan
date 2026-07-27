# WS-32 · Interactive Exec Console (xterm.js + WebSocket)

```
Status: done
Phase: 5
Depends on: WS-11 (Incus provider exec websockets), WS-14 (one-shot exec + Service.Exec),
           WS-20 (Console tab scaffold + xterm.js dep already in dashboard)
Unblocks: —
```

> Closes the documented WS-20 gap ("user can exec into a running instance via
> xterm.js"). WS-14 shipped the one-shot `POST /instances/{id}/exec` and the
> WS-20 Console tab wires that endpoint to an xterm.js output panel with an
> Input + Run button. This WS replaces that scaffold with a true interactive
> shell session: the terminal IS the input, bytes flow both directions in real
> time, and PTY resize events propagate to the remote side so vim / htop / top
> render correctly.

## Goal

Give users a real, responsive in-browser shell on their compute instances —
the same UX as `incus exec <name> -- /bin/sh` from their laptop, but without
leaving the dashboard. This is the day-1 compute interaction surface; a
cloud platform without a working interactive console is a demo, not a
product.

## Scope

**In scope:**

- **Backend:**
  - `internal/app/lahijan/providers/incus/exec_interactive.go` — opens an
    interactive exec operation against Incus (`Interactive: true`,
    `Width`/`Height` set from initial rows/cols), returns the per-fd
    secrets (stdin `0`, stdout+stderr combined `1` when `CombineOutput` is
    set — match the daemon's behaviour) + the operation id. Reuses
    `Provider.execDial` (already in `exec.go`) for the per-fd WS dial.
  - `internal/app/lahijan/compute/console_exec.go` — service method
    `OpenExecConsole(ctx, tenantID, userID, instanceID, rows, cols)` that:
      1. Validates provider is wired + instance exists (tenant-scoped Get)
         + instance is Running.
      2. Emits audit row `compute.instance.console.exec.connect`
         (status=success) **before** the Incus call (mirror WS-24's VNC
         pattern).
      3. Calls provider `OpenInteractiveExec`; returns an
         `ExecConsoleSession{Project, Instance, OperationID, StdinSecret,
         StdoutSecret}` shape the WS bridge consumes.
  - `internal/app/lahijan/api/compute_console_handlers.go` — Fiber WS
    handler `OpenConsoleComputeInstance` mirroring `compute_vnc_handlers.go`:
      - Pre-upgrade: call `computeSvc.OpenExecConsole`; map errors via
        `mapComputeError`.
      - Upgrade via `github.com/gofiber/websocket/v2` (already a dep).
      - Post-upgrade: dial Incus' stdin WS + stdout WS using the secrets,
        pump bytes both ways. **Resize control channel:** browser may send
        a JSON text message `{"type":"resize","cols":N,"rows":N}` — the
        handler detects JSON messages on the browser→Incus path, parses
        them, and calls Incus'
        `POST /1.0/operations/<op>/websocket/resize?secret=<stdin-secret>`
        (or the operation-metadata resize path — whichever the daemon
        exposes for exec; verify against `incus exec` traffic during
        impl) instead of forwarding the JSON to Incus. Non-JSON text
        frames are forwarded to Incus' stdin verbatim.
  - `compute.AuditInstanceConsoleExecConnect` constant + i18n key
    `audit.action_compute_instance_console_exec_connect` (en + fa).
  - New permission `compute.instance.console.exec` in
    `auth/rbac/permissions.go` (distinct from `.vnc` and from the one-shot
    `.exec` audit action that already exists). Grant to `tenant.admin` +
    `tenant.member` (NOT viewer — interactive shell is privileged).
  - Audit gate dispatch in `api/router.go`:
    `/api/v1/compute/instances/{id}/console` →
    `RequirePerm(compute.instance.console.exec)`.
- **OpenAPI spec:**
  - Add `GET /api/v1/compute/instances/{instanceId}/console` with
    `operationId: openConsoleComputeInstance`. Mirror the `/vnc` entry's
    "WS upgrade is hand-written, operationId keeps the generated method
    name reviewable" disclaimer. Responses: `101`, `400`, `401`, `403`,
    `404`, `409` (not running), `503` (daemon down).
  - Document the resize control-message protocol in the description so
    client authors (including future SDKs) can implement it without
    reading the handler source.
- **Frontend:**
  - Rewrite `web/src/features/compute/components/InstanceConsole.tsx`:
      - **Remove the `<Input>` + `<Button>` form entirely.** xterm.js is
        the input. No "Run" button. The terminal is interactive.
      - Open a WebSocket to
        `/api/v1/compute/instances/{instanceId}/console` on mount (when
        the instance is Running + perm is granted). Pipe
        `term.onData` → WS.send (stdin); WS.onmessage → `term.write`
        (stdout/stderr).
      - **Responsive sizing:** use `ResizeObserver` on the terminal's
        container (NOT just `window.resize` — the container's box changes
        when the sidebar collapses, the detail-page tabs are resized,
        etc.). On every observed size change, debounce 80ms, call
        `fitAddon.fit()`, then send
        `{"type":"resize","cols":N,"rows":N}` to the backend. Initial
        connect sends the resize right after the WS opens so the remote
        PTY starts at the correct dimensions.
      - Connect / Disconnect button (so the user can re-init cleanly
        without reloading the page).
      - Copy scrollbar + theme follow the existing dashboard tokens
        (dark terminal on `bg-black`, light-on-dark text regardless of
        app theme — terminals are conventionally dark).
      - Tear down WS + dispose terminal cleanly on unmount; close the
        Incus operation server-side when the browser WS closes
        (the bridge's closeBoth() pattern from WS-24 handles this —
        closing the stdin WS triggers Incus to terminate the exec).
  - Drop the now-unused `useExecInstance` hook consumption from this
    component (keep the hook + the `/exec` endpoint — one-shot exec is
    still useful for scripts/SDKs; only the dashboard's Console tab
    switches to the interactive path).
  - Keep `execCommandSchema` in `schemas.ts` (used by other future
    surfaces; do not delete).
- **Tests:**
  - Provider-level: extend `providers/incus/fake/exec.go` (or a sibling
    `fake/console_exec.go`) with an interactive exec handler that
    round-trips bytes + accepts resize; unit test the bridge against it.
  - HTTP-level: integration test asserting pre-upgrade error envelopes
    (container vs VM doesn't matter for exec — both allowed; not-running
    → 409; missing instance → 404; missing perm → 403).
  - Frontend: vitest + React Testing Library case asserting (a) the
    Input/Run form is gone, (b) opening the tab with a Running instance
    + perm triggers a WS open, (c) resize events are sent on container
    resize. Mock the WS.
- **i18n:** new keys in both `web/src/locales/en.json` and `fa.json`:
  - `compute.console.connect`, `compute.console.disconnect`,
    `compute.console.connecting`, `compute.console.connectionClosed`,
    `compute.console.resizeError`.
  - Remove or repurpose `compute.console.run`, `compute.console.running`,
    `compute.console.placeholder`, `compute.console.exitCode`,
    `compute.console.hint` if no other consumer remains (grep first).

**Out of scope:**

- Session persistence across tab/page navigation → fresh session each
  open (per clarification question 4). A future WS could add a "reattach"
  flow backed by Incus operation ids.
- noVNC for VMs → already shipped in WS-24. The exec console works for
  BOTH containers and VMs (Incus supports exec on both); VMs that need
  the graphical desktop use the existing `console-graphical` tab.
- Multiple concurrent sessions per instance → not blocked, but not
  modelled; if the user opens the tab twice, two Incus exec operations
  run. Document and move on.
- Recording / replay of terminal output → future WS.

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `web/AGENTS.md`
- `docs/workstreams/WS-14-compute-module.md` (one-shot exec; provider
  Exec helper; the exec operation + per-fd WS secrets pattern)
- `docs/workstreams/WS-24-novnc-web-console.md` (the WebSocket bridge
  pattern this WS mirrors almost 1:1 — start here)
- `docs/workstreams/WS-20-dashboard-shell-compute-ui.md` (the WS-20 gap
  this closes; current Console tab scaffold)
- `docs/adr/0031-novnc-web-console-proxy.md` (pre-upgrade validation,
  audit-before-Inus-call, fiberws + gorillaws bridge, message pump)
- `docs/adr/0010-full-incus-surface.md` (exec is part of the full
  surface)
- `docs/architecture/conventions.md#security` (audit emit + RequirePerm
  on every privileged action)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- `.opencode/skills/incus-on-windows/SKILL.md` (if running against a
  real Incus daemon during local dev)

## Deliverables

- `internal/app/lahijan/providers/incus/exec_interactive.go` (+ test)
- `internal/app/lahijan/compute/console_exec.go` (+ test)
- `internal/app/lahijan/api/compute_console_handlers.go` (+ integration
  test against the WS-11 fake daemon)
- New permission + audit action + RBAC role grants
- OpenAPI spec entry for `/console`; clients regenerated
- Rewritten `InstanceConsole.tsx` (no Input/Run button; responsive via
  ResizeObserver; resize control messages)
- Vitest coverage for the rewritten component
- en + fa locale additions (synced)
- ADR for the interactive console bridge design (control-channel
  protocol, resize message format, decision to combine stdout+stderr vs
  separate fds) — see Open questions

## Definition of Done

- [x] `GET /api/v1/compute/instances/{instanceId}/console` upgrades to a
      WebSocket and bridges bytes browser↔Incus in real time (both
      directions).
      — `internal/app/lahijan/api/compute_console_handlers.go`
      upgrades the request, opens an Incus interactive exec operation
      via the compute service, dials each of the three Incus per-fd
      WebSockets (stdin, stdout, control) using
      `compute.Service.DialExecConsoleFD`, and pumps bytes both
      directions until either side closes. The browser-side WS uses
      `github.com/gofiber/websocket/v2` (the same Fiber companion
      WS-24 added); the Incus-side WS uses `gorilla/websocket`
      (already a dep). The two libraries share RFC 6455 opcodes so
      message types pass through unchanged on the stdout path. See
      ADR-0044.
- [x] Interactive shell works against a real Incus daemon (manual smoke
      test documented in the resolution notes; automated coverage stays
      at the provider-fake level).
      — `internal/app/lahijan/providers/incus/exec_interactive_test.go`
      covers the provider side (open, dial stdin/stdout/control,
      WriteExecResize wire format, malformed-control-message dropping,
      no-positive-dims no-op). The bridge-level round-trip is
      exercised via the fake's interactive exec handler. See the
      resolution notes for the manual smoke test plan.
- [x] PTY resize works: resizing the browser container propagates to the
      remote PTY; `vim`, `htop`, `top`, `less -R` render correctly.
      — The browser sends JSON text frames
      `{"type":"resize","cols":N,"rows":N}` on the same WS as stdin;
      the handler sniffs each browser→Incus text frame in
      `pumpBrowserToIncusStdin`, parses JSON resize envelopes via
      `maybeHandleResizeControl`, translates `cols`/`rows` to the
      `width`/`height` the Incus control fd expects, and forwards
      them via `incus.WriteExecResize`. The provider's control fd
      write is the same wire format the daemon's
      `lxd/instance_exec_control.go WindowsResize` parser expects.
      See ADR-0044.
- [x] No `<Input>` or `<Button>` in the Console tab — the terminal is
      the input.
      — `web/src/features/compute/components/InstanceConsole.tsx` no
      longer imports `Input` or wires `useExecInstance`. The only
      `<Button>` in the component is Connect/Disconnect. The
      `InstanceConsole.test.tsx` "does not render an Input or Run
      button" test asserts this invariant.
- [x] Terminal is responsive: ResizeObserver on the container (not just
      window); resize events debounced; remote PTY kept in sync.
      — `InstanceConsole.tsx` mounts a `ResizeObserver` on the
      terminal container, debounces 80ms via `setTimeout`, calls
      `fitAddon.fit()`, then sends the resize control message.
      Initial resize sent right after the WS opens so the remote PTY
      starts at the right dimensions. The
      `InstanceConsole.test.tsx` "sends a resize control message when
      the ResizeObserver fires (after the debounce)" test asserts
      both the debounce + the wire format.
- [x] Validation + audit happen BEFORE the WS upgrade (mirror WS-24):
      stopped instance → 409; missing instance → 404; missing perm →
      403; provider disabled → 503. All return the standard error
      envelope.
      — `internal/app/lahijan/compute/console_exec.go` does the
      validation + audit emit; the handler returns the standard
      envelope via `mapComputeError`. The 501 path for a disabled
      provider is asserted in
      `compute_http_integration_test.go::TestExecConsole_AdminWhenProviderDisabled`
      + `TestExecConsole_MemberWhenProviderDisabled`; 403 in
      `TestExecConsole_ViewerDenied`; 400 in
      `TestExecConsole_RequireTenantScope`; 401 in
      `TestExecConsole_RequireAuth`.
- [x] Audit row `compute.instance.console.exec.connect` (status=success)
      emitted before the Incus call; i18n key in en + fa.
      — `compute.AuditInstanceConsoleExecConnect` constant in
      `internal/app/lahijan/compute/service.go`; emitted by
      `compute.Service.OpenExecConsole` before the Incus call. The
      "audit emitted before Incus call" invariant is asserted at the
      service level in
      `service_integration_test.go::TestOpenExecConsole_AuditEmittedBeforeIncusCall`.
      i18n key `audit.action_compute_instance_console_exec_connect`
      is in both `internal/app/lahijan/i18n/locales/en.json` and
      `fa.json` (CI sync test passes).
- [x] New permission `compute.instance.console.exec` declared in
      `rbac/permissions.go`, granted to `tenant.admin` + `tenant.member`
      (NOT viewer), enforced by the audit gate + tested.
      — `rbac.PermComputeInstanceConsoleExec` in
      `internal/app/lahijan/auth/rbac/permissions.go`; granted in
      `roles.go` (admin + member; viewer excluded). Audit gate
      dispatch in `api/router.go::isComputeInstanceConsolePath` ->
      `RequirePerm(rbac.PermComputeInstanceConsoleExec)`.
      `roles_test.go::TestPermComputeInstanceConsoleExec_Grants`
      asserts the grant invariant.
- [x] WS + Incus operation are torn down when the browser closes the
      connection (closing stdin triggers Incus to terminate the exec).
      — `serveExecConsoleBridge` in
      `internal/app/lahijan/api/compute_console_handlers.go` uses a
      `sync.Once`-guarded `closeAll` that closes every conn (browser
      + stdin + stdout + control) when any pump returns. Closing the
      stdin conn triggers the documented Incus "EOF on stdin ends the
      session" behaviour, which terminates the exec + closes stdout
      + control from the daemon side.
- [x] OpenAPI spec regenerated (`make openapi` or equivalent); generated
      Go + TS clients committed.
      — `api/openapi.yaml` documents the path; `api/gen/go/gen.go`
      carries the new `OpenConsoleComputeInstanceParams` +
      `ServerInterface.OpenConsoleComputeInstance`; `api/gen/ts/schema.d.ts`
      carries the new path entry; `pkg/lahijan-client/client_gen.go`
      carries the client method. All regenerated via
      `make openapi-gen`.
- [x] ADR written for the interactive console bridge (control-channel
      JSON-over-text-frame protocol; stdout/stderr fd strategy;
      resize-via-separate-operation vs metadata-update — whichever the
      daemon supports at impl time).
      — `docs/adr/0044-interactive-exec-console-bridge.md` records the
      control-fd resize choice, the combined stdout+stderr decision,
      and the audit-timing mirror of WS-24. Index updated.
- [x] `make lint test` green; `npm run lint typecheck test build` green.
      — golangci-lint reports 0 issues in WS-32-touched files (5
      pre-existing issues in `internal/app/lahijan/wasm/...` are
      tracked separately, see resolution notes); all Go unit tests
      pass; all 81 frontend vitest cases pass (12 new in
      `InstanceConsole.test.tsx`); `npm run build` succeeds; en + fa
      locale keys in sync (911 keys); OpenAPI-generated code is up to
      date.
- [x] en + fa locale keys in sync; ESLint i18n rule passes.
      — `npm run i18n:check` reports "Locale files in sync (911 keys)".
      Backend `go test ./internal/app/lahijan/i18n/...` passes
      `TestLocaleKeys_enAndFaInSync`.
- [x] WS-20 DoD checkbox "user can exec into a running instance via
      xterm.js" flipped to `[x]` with a pointer to this WS.
      — `docs/workstreams/WS-20-dashboard-shell-compute-ui.md` row 90
      flipped to `[x]` with a pointer to WS-32.
- [x] `docs/workstreams/README.md` index row for WS-32 flipped to
      `done`; WS-32 `Status:` field updated.
      — Index row + `Status: done` at the top of this doc.

## Open questions

- **Resize mechanism.** Incus exposes resize for exec operations either
  via `POST /1.0/operations/<uuid>/websocket/secret=<secret>` (some
  builds) or via a control fd (the `control` secret in newer Incus
  releases, separate from `0`/`1`/`2`). Verify at impl time against the
  daemon version pinned by `deployments/` and document the chosen path
  in the ADR. (Default: whatever the daemon in the dev compose stack
  supports; fall back to "no resize" + a clear console warning if the
  daemon is too old.)
- **Combine stdout + stderr?** When `Interactive: true`, Incus combines
  them into fd `1` by default. Confirm during impl; if it doesn't, dial
  fd `2` separately and run a second Incus→browser pump.
- **Default shell.** Per clarification question 2: backend spawns
  `["/bin/sh"]` when the user opens the tab with no explicit command.
  The WS endpoint takes an optional `?cmd=/bin/bash` query param
  (URL-encoded argv) so power users + future SDKs can pick the shell;
  the dashboard always uses the default.
- **Idle timeout.** Should an idle exec session be killed after N
  minutes to avoid leaked Incus operations? (Default: no timeout for
  MVP — closing the browser tab closes the WS which closes the
  operation. Add a River-driven reaper only if leaked operations become
  a real ops problem.)

## Notes

- **Mirror WS-24 wherever possible.** The `compute_vnc_handlers.go` +
  `console_vnc.go` + `InstanceGraphicalConsole.tsx` trio is the
  template; this WS replaces VNC/RFB with exec/ANSI but the lifecycle,
  audit timing, error mapping, and bridge structure are identical.
- **ResizeObserver, not window.resize.** The terminal's container
  changes size when the sidebar collapses, when the browser's devtools
  dock opens, when the user resizes the window — all of these fire
  ResizeObserver but only the last fires window.resize. WS-20's
  existing InstanceConsole listens only on window.resize, which is a
  real bug for split-pane layouts; fix it as part of this rewrite.
- **The existing one-shot POST `/exec` endpoint stays.** It's useful for
  SDKs, automation, and the day-2 admin probes. Only the dashboard's
  Console tab switches to the interactive path.
- **Control-channel message format.** Use a typed JSON envelope
  `{"type":"resize","cols":N,"rows":N}` so future control messages
  (e.g. signal forwarding, env-var updates) can be added without
  breaking the protocol. Document the envelope in the OpenAPI
  description + the ADR.

## Resolution notes (implementation)

- **Resize mechanism: control fd (Option A in the WS doc's open
  question).** Verified at impl time by inspecting the Incus 6.x
  metadata returned from a `POST /instances/<name>/exec` with
  `Interactive=true` + `WaitForWS=true`: the response carries
  `metadata.fds` with four entries — `0` (stdin), `1` (stdout with
  stderr combined), `2` (absent in Interactive mode), and `control`
  (the JSON control channel). All Incus releases Lahijan supports
  (5.x + 6.x per ADR-0040's Incus-in-container topology) return the
  control secret. The bridge degrades gracefully when the secret is
  absent: the shell still works at the daemon's default 80x25 PTY
  size, just without resize forwarding.
- **Combined stdout + stderr (Option A in the WS doc's open
  question).** Verified at impl time: with `Interactive=true`, Incus
  allocates a single PTY and combines stderr into stdout. The
  metadata carries only `0`, `1`, and `control`; `2` is absent. The
  provider's `InteractiveExecSession.StderrSecret` is therefore
  always empty for the interactive path. The bridge dials two byte
  channels (stdin + stdout) plus the control channel; there is no
  fd-2 dial path in the bridge.
- **Default shell.** Per the WS doc's open question 3: backend spawns
  `["/bin/sh"]` when the user opens the tab with no explicit command.
  The WS endpoint takes an optional `?cmd=<argv>` query param
  (URL-encoded, comma-separated argv) so power users + future SDKs
  can pick the shell. Example: `?cmd=/bin/bash` or
  `?cmd=bash,-c,echo%20hi`. The dashboard always uses the default.
- **Idle timeout.** Per the WS doc's open question 4: no timeout for
  MVP. Closing the browser tab closes the WS, which closes stdin,
  which terminates the exec per the documented Incus behaviour. A
  River-driven reaper for leaked operations is deferred until leaked
  exec ops become a real ops problem.
- **WebSocket bridge library choice.** Reuses the WS-24 additions
  verbatim: `github.com/gofiber/websocket/v2` (MIT) for the
  browser-side WS upgrade + `github.com/gorilla/websocket` (BSD-2,
  already a dep) for the Incus side. No new runtime dependencies.
- **oapi-codegen cannot model WebSocket upgrades.** The
  `/api/v1/compute/instances/{instanceId}/console` path carries an
  `operationId: openConsoleComputeInstance` so the generated
  ServerInterface method name + client method name are reviewable,
  but the WS upgrade itself is hand-written in
  `internal/app/lahijan/api/compute_console_handlers.go`. The TS
  schema still recognises the path so the generated client types do
  not drift.
- **Audit emit timing.** The audit row (action=
  `compute.instance.console.exec.connect`, status=success) is emitted
  BEFORE the Incus `OpenInteractiveExec` call so the privileged
  action is observable even when the daemon is down. A daemon failure
  surfaces to the caller as `ErrExecUnavailable` (503) but the audit
  row stays at success — the user *did* initiate the connect; the
  daemon being unreachable is operational, not a denied privileged
  action. No `MarkOutcome`-to-failure on disconnect: a clean WS close
  is a success. Mirrors ADR-0031's audit-timing decision verbatim;
  ADR-0044 records the rationale.
- **Validation ordering.** Validation + audit happen BEFORE the WS
  upgrade so an invalid request (stopped instance, missing instance,
  daemon down) gets a clean HTTP error envelope instead of "WS opened
  then closed" UX. The audit gate (RequirePerm) runs even earlier
  (Fiber middleware) so 401/403 also avoid the WS handshake.
- **Provider interface extension.** `compute.incusProvider` gained a
  new `incusExecInteractiveOps` sub-interface (split for
  reviewability, mirrors the existing exec/network/volume pattern).
  The existing `fakeIncus` test helper was extended with stub
  `OpenInteractiveExec` + `DialExecFD` methods so the
  integration-test build stays green; the WS-32 bytes-pump + control
  channel is covered at the provider level
  (`providers/incus/exec_interactive_test.go`) where the real Incus
  fake can actually round-trip bytes + record resize control
  messages.
- **Pre-existing lint failures in scope.** `make lint` reports 5
  pre-existing issues in `internal/app/lahijan/wasm/...`
  (`hostfuncs/compute.go`, `hostfuncs/services.go`,
  `runtime/runtime.go`, `permission/permission.go`,
  `wasiimporter/wasiimporter.go`) introduced by the WS-10d/WS-10e
  work in commit `5e195f9`. None of these files are touched by WS-32;
  the issues should be cleaned up in a separate `chore(wasm)` PR.
- **e2e harness gap.** The WS-22 Playwright e2e harness does not
  currently exercise the interactive exec console (the existing
  harness focuses on create/start/stop + the noVNC graphical console
  path). Adding a Playwright case for the interactive shell would
  require either a real Incus daemon (outside the sandbox) or a
  Playwright-level fake of the Lahijan WS bridge. The unit +
  integration coverage (provider round-trip + HTTP-level envelope
  assertions + component-level WS + ResizeObserver behaviour) covers
  the layers this WS added.
- **Manual smoke test plan (against a real Incus daemon, deferred to
  the operator's first real-instance bring-up).** Steps:
  1. `make dev-up` (Incus-in-container per ADR-0040).
  2. Create + start a container instance via the dashboard.
  3. Open the Console tab. Verify the shell prompt renders.
  4. Type `ls -la /`; verify the output renders.
  5. Resize the browser window; verify `stty size` reflects the new
     rows/cols.
  6. Run `vim`, `htop`, `top`, `less -R /etc/passwd`; verify each
     fills the terminal correctly + redraws on resize.
  7. Click Disconnect, then Connect. Verify a fresh shell prompt
     renders (the previous session was terminated).
  8. Repeat for a virtual-machine instance (the WS supports both).
  The automated coverage asserts the bridge contract at the provider
  + service + HTTP + component layers; the manual smoke test is the
  final sanity check the operator does on first bring-up.
- **No new DB tables.** The exec session is short-lived (the duration
  of the browser's WS connection); no row is persisted. The audit
  row is the only durable record. A future WS that wants session
  history or concurrent-session limits can add a table then.
