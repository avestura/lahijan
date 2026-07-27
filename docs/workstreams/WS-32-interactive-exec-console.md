# WS-32 · Interactive Exec Console (xterm.js + WebSocket)

```
Status: pending
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

- [ ] `GET /api/v1/compute/instances/{instanceId}/console` upgrades to a
      WebSocket and bridges bytes browser↔Incus in real time (both
      directions).
- [ ] Interactive shell works against a real Incus daemon (manual smoke
      test documented in the resolution notes; automated coverage stays
      at the provider-fake level).
- [ ] PTY resize works: resizing the browser container propagates to the
      remote PTY; `vim`, `htop`, `top`, `less -R` render correctly.
- [ ] No `<Input>` or `<Button>` in the Console tab — the terminal is
      the input.
- [ ] Terminal is responsive: ResizeObserver on the container (not just
      window); resize events debounced; remote PTY kept in sync.
- [ ] Validation + audit happen BEFORE the WS upgrade (mirror WS-24):
      stopped instance → 409; missing instance → 404; missing perm →
      403; provider disabled → 503. All return the standard error
      envelope.
- [ ] Audit row `compute.instance.console.exec.connect` (status=success)
      emitted before the Incus call; i18n key in en + fa.
- [ ] New permission `compute.instance.console.exec` declared in
      `rbac/permissions.go`, granted to `tenant.admin` + `tenant.member`
      (NOT viewer), enforced by the audit gate + tested.
- [ ] WS + Incus operation are torn down when the browser closes the
      connection (closing stdin triggers Incus to terminate the exec).
- [ ] OpenAPI spec regenerated (`make openapi` or equivalent); generated
      Go + TS clients committed.
- [ ] ADR written for the interactive console bridge (control-channel
      JSON-over-text-frame protocol; stdout/stderr fd strategy;
      resize-via-separate-operation vs metadata-update — whichever the
      daemon supports at impl time).
- [ ] `make lint test` green; `npm run lint typecheck test build` green.
- [ ] en + fa locale keys in sync; ESLint i18n rule passes.
- [ ] WS-20 DoD checkbox "user can exec into a running instance via
      xterm.js" flipped to `[x]` with a pointer to this WS.
- [ ] `docs/workstreams/README.md` index row for WS-32 flipped to
      `done`; WS-32 `Status:` field updated.

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
