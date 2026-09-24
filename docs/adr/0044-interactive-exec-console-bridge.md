# ADR-0044: Interactive exec console bridge — control-fd resize, combined stdout/stderr, audit timing

- **Status:** Accepted
- **Date:** 2026-07-27
- **Deciders:** maintainer
- **Related:** WS-32, ADR-0031 (noVNC bridge — pattern this WS mirrors 1:1)
- **Amended:** 2026-09-17 — the original text described the interactive
  per-fd metadata as `0`/`1`/`control` (a separate stdout). Bringing the
  bridge up against a real Incus daemon showed the daemon returns
  `0`/`control` only: fd `0` is the **bidirectional** PTY master. The
  three decisions below are unchanged; the wire-protocol description and
  the fd counts are corrected in place, and four implementation
  constraints found in the same pass are recorded under
  [Amendment notes](#amendment-notes-2026-09-17).
- **Amended:** 2026-09-24 — the control-fd wire format below was wrong;
  see [Amendment notes (2026-09-24)](#amendment-notes-2026-09-24).

## Context

WS-32 ("Interactive Exec Console") adds a browser-based interactive
shell on compute instances. The browser-side xterm.js client must
connect to Incus' interactive exec operation without exposing the word
"Incus" to end users (pillar 1) and without bypassing Lahijan's RBAC +
audit layer (pillar 7). WS-24 (noVNC) already proved out the
WebSocket-bridge pattern for VM graphical consoles; this WS mirrors it
almost verbatim but with three sub-decisions the noVNC bridge did not
have to make.

1. **Resize mechanism.** A browser-side terminal needs to tell the
   remote PTY "I'm now 132 columns by 50 rows" every time the user
   resizes the window, collapses the sidebar, opens devtools, or
   resizes the tab. `vim`, `htop`, `top`, and `less -R` render
   incorrectly (or scramble on the next redraw) if the PTY dimensions
   drift from what the browser thinks it is showing. Incus exposes
   resize for exec operations via two mechanisms:

   - **Option A — control fd (modern).** When `Interactive=true` and
     `WaitForWS=true`, the daemon mints an additional per-fd secret
     named `"control"` alongside the data fd `"0"`. The control fd
     accepts JSON messages of shape
     `{"command":"window-resize","args":{"width":"N","height":"N"}}`
     (Incus' `api.InstanceExecControl`; corrected 2026-09-24) (plus `signal` for
     signal forwarding). All Incus releases that support interactive
     exec (5.x, 6.x — the only versions Lahijan supports per
     ADR-0040) return the control secret.
   - **Option B — separate REST POST.** Some builds expose resize via
     `POST /1.0/operations/<uuid>/websocket?secret=<stdin-secret>`.
     Older, less ergonomic, requires a separate HTTP client path.
   - **Option C — re-open the operation.** Close + reopen the exec at
     the new dimensions. Loses in-flight state (the user's shell
     session, scrollback, environment). Not viable for an interactive
     session.

2. **Combine stdout + stderr?** Incus' exec behaviour differs between
   `Interactive=false` (the one-shot capture path WS-14 ships) and
   `Interactive=true` (the PTY-backed shell WS-32 adds):

   - `Interactive=false` -> the daemon returns three per-fd secrets
     (`0`/`1`/`2`) and the caller dials + pumps each.
   - `Interactive=true` -> the daemon allocates a single PTY and
     combines stderr into stdout — *and* merges the result back onto
     the input fd. The metadata carries only `0` and `control`; both
     `1` and `2` are absent. Fd `0` is the PTY master: the client
     writes keystrokes to it and reads the shell's output from it on
     the same websocket. This is the same UX as
     `incus exec <name> -- /bin/sh` on the CLI.

3. **Where does the audit row land relative to the Incus call?** Per
   pillar 7 every privileged action emits an audit event. Opening an
   interactive shell is privileged (it grants arbitrary code execution
   inside the instance). WS-24 decided "emit BEFORE the Incus call,
   never mark failure on disconnect" for the VNC path; this WS had to
   decide whether to follow the same rule for exec.

   - **Option A — mirror WS-24 verbatim.** Audit row (status=success)
     lands before the Incus call; a daemon failure surfaces as
     `ErrExecUnavailable` (503) but the audit row stays at success.
     The user *did* initiate the connect; the daemon being
     unreachable is operational, not a denied privileged action.
   - **Option B — emit + MarkOutcome to failure on daemon failure.**
     More precise but doubles the write rate (one row + one outcome
     per failed connect), complicates the audit UI (a "successful
     action that failed" is a confusing event), and diverges from
     the WS-24 precedent.

## Decision

WS-32 implements the interactive exec console as follows:

1. **Resize: Option A — the control fd.** The provider's
   `OpenInteractiveExec` returns a second secret (`ControlSecret`)
   alongside the data fd. The bridge opens the control fd once at
   session start and writes JSON resize messages to it for the
   duration of the browser's WS connection. The browser sends the
   resize on the same WS as stdin (JSON envelope
   `{"type":"resize","cols":N,"rows":N}`); the bridge sniffs each
   browser→Incus text frame, parses JSON resize envelopes,
   translates `cols`/`rows` (browser convention) to `width`/`height`
   (Incus control-fd convention), and forwards. Non-JSON text frames
   go to stdin verbatim. When the daemon does not return a control
   secret (very old builds), the bridge degrades gracefully: the
   shell still works, but vim/htop render at the daemon's default
   80x25 PTY size.

2. **Stdout + stderr: combined onto the bidirectional data fd.** The
   provider always passes `Interactive=true`, so the daemon allocates a
   single PTY and merges stdout + stderr back onto fd `0`.
   `OpenInteractiveExec` returns `StdoutSecret=""` and
   `StderrSecret=""` to signal this; the bridge dials **one** byte
   channel (fd `0`) plus the control channel, and runs both pumps over
   that one conn — one goroutine reading, one writing, which
   gorilla/websocket permits. There is no fd-1 or fd-2 dial path in the
   bridge. Stdout is treated as optional rather than required: an empty
   `StdoutSecret` is the normal interactive case, not an error.

3. **Audit timing: mirror WS-24 verbatim.** The service emits the
   audit row (`action=compute.instance.console.exec.connect`,
   `status=success`) BEFORE the Incus call. Daemon failure surfaces
   as `ErrExecUnavailable` (503) but the audit row stays at success;
   no `MarkOutcome`-to-failure on disconnect. A clean WS close is a
   success.

This ADR reuses ADR-0031's "fiberws + gorillaws bridge, pre-upgrade
validation, audit gate dispatch" pattern verbatim — the only
behavioural differences are: (a) two Incus-side WS (the bidirectional
data fd + control) instead of one (RFB), (b) the JSON control-message
sniffer on the browser→Incus path, and (c) no VM-only gate (exec works on
both containers and VMs).

## Consequences

- **Positive:** the resize path matches what `incus exec` does on the
  CLI — no translation layer, no fake "resize via REST POST" path to
  maintain. Vim, htop, top, and less -R render correctly the moment
  the browser's ResizeObserver fires.
- **Positive:** the audit trail captures every interactive shell
  session-open as a distinct, queryable event (`action =
  compute.instance.console.exec.connect`), even when the daemon is
  down. Operators can answer "who opened a shell on this instance
  and when" without correlating Incus logs.
- **Positive:** the bridge is pure Go in-process — no new process to
  supervise, no new port, no extra container. Identical to ADR-0031's
  topology.
- **Positive:** the control-channel JSON envelope
  (`{"type":"resize","cols":N,"rows":N}`) is typed + extensible.
  Future control messages (signal forwarding, env-var updates) land
  as a new `type` value without breaking the wire protocol.
- **Negative:** the WS endpoint is not modelled by oapi-codegen (it
  cannot model WebSocket upgrades) so the handler is hand-written in
  `internal/app/lahijan/api/compute_console_handlers.go`. Mitigation:
  the path has an `operationId` (`openConsoleComputeInstance`) so the
  generated ServerInterface method name is reviewable; the TS schema
  recognises the path so the generated client types do not drift.
- **Negative:** resize forwarding depends on the daemon returning a
  `control` secret. Very old Incus builds do not; the bridge detects
  this and silently skips resize (the shell still works, the user
  just sees the daemon's default PTY dimensions). Documented in the
  OpenAPI description.
- **Neutral:** session-cookie auth on WebSocket works for same-origin
  deployments (per ADR-0030). A future WS that splits the dashboard
  onto a different origin would need to mint a short-lived WS token;
  out of scope here.
- **Neutral:** multiple concurrent sessions per instance are not
  modelled (the WS doc lists this as out of scope). If the user opens
  the tab twice, two Incus exec operations run. Documented + a
  follow-up WS could add a reattach flow backed by Incus operation
  ids if leaked operations become a real ops problem.

## Compliance

- `internal/app/lahijan/providers/incus/exec_interactive.go` defines
  `OpenInteractiveExec` (returns the data-fd secret + `control`) + `DialExecFD` (dials any per-fd WS) + `WriteExecResize`
  (formats + writes the JSON resize control message). All three open
  OTel spans via the package-local tracer (per ADR-0016).
- `internal/app/lahijan/providers/incus/fake/exec.go` extends the
  httptest fake to dispatch on `Interactive=true`, mint the daemon's
  real two-secret layout (`0` + `control`), echo input frames back on
  fd `0` the way a PTY does, and record resize control messages so the
  bridge unit test can assert the control channel actually forwarded
  them. The fake previously minted a separate `1`, which made the whole
  test suite pass against a layout the daemon never produces.
- `internal/app/lahijan/compute/console_exec.go` is the service-layer
  orchestration (lookup → audit emit → Incus call → return session).
  `compute.AuditInstanceConsoleExecConnect` is the audit action
  constant; the service emits BEFORE the Incus call.
- `internal/app/lahijan/api/compute_console_handlers.go` is the
  WebSocket upgrade handler. Pre-upgrade validation + audit emit +
  `mapComputeError` translation; post-upgrade bridge (data fd +
  control fd) with the control-fd resize sniffer. Every catch-all logs the underlying
  error (with type, op id, fd name) before returning the generic
  envelope per the backend AGENTS.md "Hard rules" section.
- The audit gate in `api/router.go` dispatches
  `/api/v1/compute/instances/{id}/console` to
  `RequirePerm(rbac.PermComputeInstanceConsoleExec)`.
- `internal/app/lahijan/auth/rbac/permissions.go` declares
  `PermComputeInstanceConsoleExec`; `auth/rbac/roles.go` grants it to
  `tenant.admin` and `tenant.member` (NOT viewer).
- `web/src/features/compute/components/InstanceConsole.tsx` is the
  browser-side xterm.js client. ResizeObserver on the terminal's
  container (NOT window.resize) with an 80ms debounce; sends
  `{"type":"resize","cols":N,"rows":N}` after every fitAddon.fit().
  Initial resize sent right after the WS opens so the PTY starts at
  the right dimensions.
- The OpenAPI spec (`api/openapi.yaml`) documents the WS path
  `/api/v1/compute/instances/{instanceId}/console` with operationId
  `openConsoleComputeInstance` + the control-channel protocol in the
  description (so future SDK authors can implement it without reading
  the handler source).

## Amendment notes (2026-09-17)

Four constraints surfaced only when the bridge was exercised against a
real Incus daemon rather than the in-process fake. Each is load-bearing
— reverting any one of them breaks the console with no compile error and
no failing test, so each now has a named regression test.

1. **Browser input must reach Incus as BINARY frames.** The interactive
   exec fd `0` silently discards TEXT-opcode frames: a text-frame
   `echo hi
` produces no echo and no execution, while the identical
   bytes sent as a binary frame are echoed and run. xterm.js's `onData`
   emits strings, so the browser side is unavoidably text; the bridge
   therefore rewrites the opcode to binary on every stdin forward. The
   payload is untouched — only the opcode changes. Pinned by
   `TestPumpBrowserToIncusStdin_AlwaysBinaryOpcode`.

2. **The websocket dialer must share the HTTP transport's config.**
   `websocket.DefaultDialer` verifies TLS and dials TCP, so every
   per-fd, VNC, and events dial failed in both supported topologies: a
   `wss://` dial against a daemon using the Incus auto-generated
   self-signed cert failed with `x509: certificate signed by unknown
   authority`, and a `ws://` dial against a Unix-socket daemon tried TCP
   and timed out. `Provider` now carries a `wsDialer` built alongside
   the HTTP transport (`NetDialContext` to the socket in
   `NewUnixClient`, the same `TLSClientConfig` in `NewRemoteClient`), and
   every dial site uses it. The WS path now accepts exactly the certs
   the HTTP path already accepts — no separate trust decision.

3. **The tenant hint needs a query-param fallback.** The browser's
   `new WebSocket(url)` exposes no header setter, so the `X-Tenant-Id`
   header the rest of the dashboard sends via the fetch interceptor
   cannot ride the upgrade request. `tenant_id` is accepted as a query
   param, ranked below the header form so a stray query value cannot
   override an explicit header. This is **not** a privilege grant: as
   with the header, the value is only a scope hint and membership is
   still enforced downstream by `RequirePerm`, so a spoofed hint reaches
   nothing the caller could not already reach. Pinned by
   `TestTenantWithResolver_QueryID_SetsContext` and
   `TestTenantWithResolver_HeaderID_BeatsQueryID`.

4. **The Vite dev/preview proxies need `ws: true`.** Without it
   http-proxy never binds the `upgrade` event, so the console handshake
   is answered by Vite's own static server (SPA fallback) and the
   terminal reports "disconnected" before a byte is pumped. Set on both
   `server.proxy` and `preview.proxy` — the WS-22 e2e harness runs
   against `vite preview`.

## Amendment notes (2026-09-24)

Verified on a real Linux host (Incus 7.4, container + KVM VM):

- **Resize wire format.** The daemon's control fd decodes
  `api.InstanceExecControl`:
  `{"command":"window-resize","args":{"width":"<cols>","height":"<rows>"}}`
  with **string** args. The `{"type":"resize","width":N,"height":N}`
  shape the bridge originally sent is silently ignored, which left every
  console PTY at 80x25. `WriteExecResize` now emits the Incus shape; the
  browser-facing envelope (`{"type":"resize","cols","rows"}`) is
  unchanged, and the fake Incus server decodes the real shape so the
  unit tests would catch a regression.

## References

- WS-32 (Interactive Exec Console)
- WS-24 (noVNC Web Console — pattern this WS mirrors 1:1)
- ADR-0031 (noVNC web console proxy — fiberws + gorillaws bridge
  design reused verbatim)
- ADR-0010 (full Incus surface — interactive exec is part of it)
- ADR-0040 (Incus-in-container topology — only Incus 5.x/6.x are
  supported, all of which return the `control` secret for Interactive
  exec)
- ADR-0030 (production deployment topology — single-host Caddy reverse
  proxy makes session-cookie auth on WebSocket work)
- [Incus exec docs](https://linuxcontainers.org/incus/docs/main/howto/instances_console/)
