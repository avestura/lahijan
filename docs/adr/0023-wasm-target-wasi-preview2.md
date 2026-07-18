# ADR-0023: WASM plugin target — WASM 1.0 (no WASI) for MVP, WASI Preview 2 future

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-10a (WASM Runtime + Permission System) has to pick the WASM target that
plugins must compile to. The choice determines:

- which languages can target Lahijan's plugin runtime,
- which host-function ABI the runtime exposes (raw import vs. canonical ABI),
- how much upstream toolchain risk Lahijan takes on (WASI Preview 2 / the
  Component Model is still in flux),
- how host functions get exposed to plugins in WS-10b.

WS-10a's "Open questions" section explicitly leaves this open and says:
"Default: wasi-preview2 for richer host interop, but this affects what
languages can target it. Write an ADR."

Options considered:

- **Option A — WASI Preview 2 / Component Model** — the modern path; richer
  interface types (records, variants, lists), canonical ABI, language-agnostic
  bindings via `wit-bindgen`. Pros: forward-looking; clean host-function
  signatures; matches where the ecosystem is heading. Cons: wazero's
  component-model support is still experimental; the WIT toolchain is in flux;
  binding generators for Go, Rust, AssemblyScript are uneven; a single
  breaking upstream change would force every plugin author to recompile.
- **Option B — WASI Preview 1** — the "classic" WASI surface (`fd_write`,
  `args_get`, `environ_get`, `clock_time_get`, etc.). Pros: well-supported by
  every language toolchain (Rust `wasm32-wasi`, Go `wasm32-wasi`, TinyGo,
  AssemblyScript, Zig); wazero ships a stable `wasi_snapshot_preview1`
  importer. Cons: the ABI is flat (i32/i64 only, no interface types); host
  functions are still raw imports; Preview 1 is being deprecated upstream.
- **Option C — Plain `wasm32-unknown-unknown` (no WASI)** — minimum viable
  surface: a module that only declares the imports Lahijan itself exposes
  (`lahijan.http_get`, `lahijan.kv_get`, etc.) and exports functions Lahijan
  calls (`on_event`, `on_request`, `run`). Pros: smallest attack surface; no
  ambient filesystem / env / clock access without an explicit grant; full
  control over the ABI; the simplest possible wazero wiring; works with every
  language that can emit raw WASM (Tinygo, Rust, AssemblyScript, Zig).
  Cons: no "free" stdlib filesystem; everything the plugin needs has to be a
  Lahijan host function (which is exactly the permission-gated surface we
  want anyway).

## Decision

Lahijan's MVP plugin runtime targets **plain `wasm32-unknown-unknown`** (Option
C). Plugins do NOT get WASI imports; the only imports a plugin can call are
the ones Lahijan itself exposes, and every one of those is gated by the
permission enforcer from WS-10a.

Rationale:

1. **Pillar 1 (transparent infrastructure) + pillar 5 (Android-style
   permissions)** — plugins should not have ambient access to a filesystem or
   environment they did not request. Plain WASM makes the import list
   *identical* to the permission list, so the manifest is the only source of
   truth.
2. **wazero v1.9** is stable for plain WASM; WASI P1 is also stable in
   wazero but adds an ambient surface we'd have to audit; the component model
   is still experimental.
3. **Toolchain breadth** — every WASM-producing language supports the plain
   target. Requiring WASI P1 or P2 narrows this (e.g. AssemblyScript has no
   first-class WASI).
4. **Future migration is purely additive** — when WASI Preview 2 stabilises
   in wazero, we can layer a P2 importer on top of the existing host-function
   surface without breaking plugins that target plain WASM. The permission
   enforcer is independent of the import ABI.

## Consequences

- **Positive:** the manifest permission list IS the full import list. There
  is no ambient `fd_write`/`environ_get`/`path_open` surface to audit.
- **Positive:** smallest possible wazero wiring; one host module built from
  the permission registry.
- **Positive:** every WASM-producing language can target Lahijan today.
- **Negative:** plugins cannot use stdlib filesystem / env APIs even if they
  wanted to; the Lahijan host-function ABI (KV, events, jobs) has to cover
  every legitimate need. WS-10b will land that surface.
- **Negative:** WASI Preview 2's interface-types ergonomics are deferred
  until the upstream + wazero story stabilises.

## Compliance

- `internal/app/lahijan/wasm/runtime/runtime.go` instantiates modules with
  `wazero.NewRuntimeConfig()` and DOES NOT register
  `wasi_snapshot_preview1.InstantiateSnapshotPreview1`. The only host module
  registered is the one built from the permission registry.
- The manifest schema (`internal/app/lahijan/wasm/manifest/manifest.go`)
  declares every import the plugin needs; an import that is not in the
  manifest is rejected at instantiation.
- A unit test (`runtime_test.go`) asserts that a module requiring WASI
  imports fails to instantiate.

## References

- WS-10a (WASM Runtime + Permission System)
- ADR-0012 (WASM plugin system in MVP scope)
- [wazero documentation](https://wazero.io/)
- [WebAssembly 1.0 specification](https://www.w3.org/TR/wasm-core-1/)
- [WASI Preview 2](https://github.com/WebAssembly/WASI/blob/main/wasi-preview2.md)
