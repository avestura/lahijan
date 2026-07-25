# WS-10d · Plugin SDK (Go) + Host Network Response Fix

```
Status: in-progress
Phase: 2
Depends on: WS-10b, WS-10c
Unblocks: —
```

## Goal

Third-party Go plugin authors should write Lahijan plugins the way they write
any Go program — calling idiomatic functions like `kv.Set("k", v)`,
`network.Post(url, body) -> Response`, `events.Subscribe("dns.record.*",
"on_dns")` — with zero `unsafe.Pointer`, zero `(ptr, len)` juggling, zero
status-code switch statements, and zero copy-pasted memory helpers. Today
every author re-derives ~60 lines of ABI plumbing (see
`examples/plugins/slack-notifier/main.go`). This WS ships a TinyGo-compatible
Go SDK module that owns all of that, and finishes the long-standing
`http_request` response-body gap on the host side so the SDK's `Network`
surface is complete.

## Scope

**In scope:**

### The SDK module (`sdk-go/`)

- New standalone Go module at `github.com/avestura/lahijan/sdk-go` (separate
  `go.mod`; zero backend dependencies; TinyGo-compilable to
  `wasm32-unknown-unknown`).
- Project-layout-compatible internal structure: package name = directory name;
  no `util` / `common` / `helpers` packages (per `project-layout.md` +
  root `AGENTS.md` Go conventions).
- Sub-packages mirroring the six host modules from WS-10b:
  - `sdk-go/kv` — `Get(key) ([]byte, error)`, `Set(key, val []byte, opts ...SetOpt) error`,
    `Delete(key) error`
  - `sdk-go/events` — `Emit(topic, payload) error`,
    `Subscribe(topicPattern, handlerName) error`, `Unsubscribe(...)`
  - `sdk-go/network` — `Do(req Request) (Response, error)` plus helpers
    `Get` / `Post` / `PostJSON`; returns full `{StatusCode, Headers, Body}`
  - `sdk-go/jobs` — `Schedule(exportName, args, runAt) error`
  - `sdk-go/api` — `RegisterHandler(method, path, handlerName) error`,
    `UnregisterHandler(...)`
  - `sdk-go/config` — `Get(key) ([]byte, error)` + typed `GetString` / `GetJSON`
  - `sdk-go/lahijan` — entrypoint registration helpers (`OnInit`, `OnEvent`,
    `OnRequest`, `OnTick`) that hide the `//export` + `(ptr,len)` decode
    boilerplate
  - `sdk-go/mem` — internal bump allocator + `(ptr,len)` round-trip helpers;
    not part of the public surface but exported for advanced users
  - `sdk-go/status` — typed errors (`ErrPermissionDenied`,
    `ErrBufferTooSmall`, `ErrNotFound`, `ErrUnavailable`, `ErrUpstream`,
    `ErrInvalidArgument`) mapped 1:1 to
    `internal/app/lahijan/wasm/hostfuncs/codes.go`; all `errors.Is`-able;
    a `StatusError` type unwraps to the numeric code
- Auto-retry on `StatusBufferTooSmall` hidden inside `kv.Get` / `config.Get` /
  `network.Do` (the SDK grows its arena and re-issues the call).
- A vendoring story that works with `tinygo build` (the SDK has no external
  deps, so `go.sum` stays empty).

### Host-side fix: `http_request` response protocol

- Finish the `TODO(WS-10c)` at
  `internal/app/lahijan/wasm/hostfuncs/network.go:172`. Currently the host
  drops `resp.Headers` + `resp.Body` and returns only the HTTP status code;
  the `respBufPtr` / `respBufCap` params documented in the samples are dead.
- **Recommended ABI (confirmed by ADR-0038):** add two inline buffer pairs to
  `http_request`, going from 8 to 12 i32 params:

  ```
  http_request(
      method_ptr, method_len,
      url_ptr, url_len,
      reqHeaders_ptr, reqHeaders_len,      // JSON map, as today
      reqBody_ptr, reqBody_len,
      respHdrBuf_ptr, respHdrBuf_cap,      // NEW: host writes JSON header map
      respBodyBuf_ptr, respBodyBuf_cap,    // NEW: host writes body bytes
  ) -> int32                               // HTTP status code (>0), or StatusXxx (<0)
  ```

  On success the host writes the response headers (JSON object, same shape as
  the request headers) into the header buffer and the body into the body
  buffer, truncating to capacity. When either buffer is too small the host
  returns `StatusBufferTooSmall` (-7) and writes nothing; the caller grows
  the buffer and retries (the SDK does this automatically; raw authors loop).
- The ADR also considers the slot-based alternative (a prior
  `set_response_buffers(hdrPtr, hdrCap, bodyPtr, bodyCap)` call that the host
  remembers) and records why inline was chosen (stateless; matches the
  already-documented 10-param shape in `docs/architecture/plugins.md`;
  simpler reasoning for raw authors).
- Update `docs/architecture/plugins.md` host-function reference + status-code
  table to match the new signature.

### Sample migration

- Rewrite `examples/plugins/slack-notifier`, `autoscaler-stub`,
  `dns-record-hook`, and `_template` to consume the SDK. Each shrinks to
  roughly its business logic (~30–40 LOC for slack-notifier vs the current
  136).
- Keep one short "raw imports" reference snippet in
  `docs/architecture/plugins.md` for non-Go authors (Rust / AssemblyScript /
  Zig) who cannot use this SDK.
- Update each sample's `go.mod` to require `github.com/avestura/lahijan/sdk-go`.
- Update the sample Makefiles (`make verify`) — they still reject WASI imports,
  which the SDK must not pull in.

### Documentation

- New section in `docs/architecture/plugins.md`: **"Using the Go SDK"** — the
  recommended path for Go authors, with a before/after comparison against the
  raw-import path.
- New appendix in `plugins.md`: **"Host ABI porting guide"** — a
  language-agnostic summary of every host module's signature, the `(ptr,len)`
  convention, the status-code table, and the retry-on-`BufferTooSmall`
  pattern, so future Rust / TS / Zig SDKs are mechanical ports.
- ADR-0038 written and Accepted (see Deliverables).

**Out of scope:**

- Non-Go SDKs (Rust, TypeScript, AssemblyScript) — future WS; the porting
  appendix is the only deliverable for them here.
- Code generation from a host-ABI schema — hand-written v1; codegen is a
  future concern once the surface stabilises.
- WASI-mode support inside the SDK — that's WS-10e. This WS designs the SDK
  so WASI support is additive (no plain-WASM-only assumptions in the
  allocator or entrypoints).
- Any change to the permission model, the manifest schema beyond the network
  ABI, or the enforcer.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0012-wasm-full-mvp.md` (the plugin system mandate)
- `docs/adr/0023-wasm-target-wasi-preview2.md` (plain `wasm32-unknown-unknown`
  target the SDK must compile to)
- `docs/adr/0024-wasm-host-function-abi.md` (the `(ptr,len)` ABI the SDK wraps)
- `docs/workstreams/WS-10b-wasm-host-functions-event-bus.md` (the host-function
  surface)
- `docs/workstreams/WS-10c-wasm-sample-plugins-marketplace.md` +
  `examples/plugins/` (the samples being rewritten)
- `docs/architecture/plugins.md` (the developer guide being extended)
- `docs/architecture/conventions.md` (Go + testing rules)
- `.opencode/skills/backend-foundations/SKILL.md` (for the host-side network
  fix only)
- `.opencode/skills/testing-conventions/SKILL.md`

## Deliverables

- `sdk-go/` new Go module with all sub-packages
- Host-side `http_request` response-body/header protocol landed in
  `internal/app/lahijan/wasm/hostfuncs/network.go` + round-trip integration
  test
- 3 sample plugins + `_template` rewritten on the SDK; `make verify` green
  for each
- "Using the Go SDK" section + "Host ABI porting guide" appendix in
  `docs/architecture/plugins.md`
- ADR-0038 written and Accepted

## Definition of Done

- [ ] `sdk-go/` is a separate Go module (`go.mod` module path
      `github.com/avestura/lahijan/sdk-go`) with zero backend imports;
      `go build ./...` and `go vet ./...` clean inside the module
- [ ] SDK compiles to `wasm32-unknown-unknown` via
      `tinygo build -target wasm`; a `make verify-sdk` target asserts the
      resulting module declares no WASI imports (mirrors the sample Makefiles)
- [ ] every public SDK function has ≥1 happy-path + ≥1 failure-path round-trip
      test through a wazero harness (the harness compiles a tiny
      SDK-consuming plugin and drives it via `runtime.Runtime.Call`)
- [ ] `kv.Get` / `config.Get` / `network.Do` auto-retry on
      `StatusBufferTooSmall` and that retry is covered by a test
- [ ] `network.http_request` writes response headers (JSON map) + body into
      the caller-supplied buffers; `hostfuncs_integration_test.go` round-trips
      a full response (status + headers + body) end-to-end
- [ ] status-code → typed-error mapping is exhaustive (one test per code in
      `codes.go`)
- [ ] slack-notifier, autoscaler-stub, dns-record-hook, and `_template`
      build via TinyGo and `make verify` passes for each
- [ ] slack-notifier's business-logic LOC drops materially (target ≤ ~50 LOC)
      — demonstrates the SDK's value
- [ ] `docs/architecture/plugins.md` has the new "Using the Go SDK" section +
      "Host ABI porting guide" appendix
- [ ] ADR-0038 written, status Accepted, listed in `docs/adr/README.md`
- [ ] `make lint test` green at repo root AND inside `sdk-go/`
- [ ] PR template checklist ticked; this WS doc's Status flipped to `done`

## Open questions

- **Response-buffer ABI: inline vs slot-based?** Recommended (this WS):
  inline — add `respHdrBuf(ptr,cap)` + `respBodyBuf(ptr,cap)` to
  `http_request`. The alternative is a prior
  `set_response_buffers(hdrPtr, hdrCap, bodyPtr, bodyCap)` call that the host
  remembers. Inline is stateless and matches the 10-param shape already
  documented in `plugins.md`; the ADR records the decision.
- **Allocator strategy inside the SDK.** A bump allocator over a reserved
  memory region is the simplest and avoids pulling in a malloc. Open: how big
  is the initial arena, and does it grow on demand via `memory.grow`? Deferred
  to ADR-0038 + the `mem` package design.
- **SDK versioning vs host ABI version.** The SDK imports are stable as long
  as the host module names + signatures are. Proposal: the SDK's `go.mod`
  version follows semver; a host-side ABI version constant is checked at
  instantiation in a future WS. Not blocking this WS.

## Notes

- The SDK must NOT assume plain-WASM-only imports — design so that WASI-mode
  plugins (WS-10e) written in TinyGo can also use it. Concretely: no reliance
  on absent WASI, and the entrypoint helpers must work whether the module
  declares WASI imports or not.
- The `_template` currently ships `readMem` + `writeString` helpers that every
  author copies. The SDK subsumes them; the `_template` rewrite becomes a thin
  `import sdk` + business logic.
- The network host fix is the one backend change in this WS. It is small and
  additive: the current 8-param call shape is a subset of the new 12-param
  shape, so a caller that passes `0, 0, 0, 0` for the new buffers still gets
  the old behaviour (status code only, nothing written). Existing tests keep
  passing.
- ADR number 0038 is the next free after 0037 (see `docs/adr/README.md`); the
  implementer confirms at write time.
- Project-layout compatibility: Lahijan uses no top-level `/pkg` dir (it
  favours `internal/` + purpose-named top-level dirs like `web/`, `website/`,
  `examples/`). A top-level `sdk-go/` as an independent importable module is
  in the same spirit and satisfies `project-layout.md`'s intent (public,
  clearly named, independently `go get`-able). The module's internal packages
  follow project-layout's naming rules. ADR-0038 records this rationale.
