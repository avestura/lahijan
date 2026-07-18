# ADR-0024: WASM Host Function ABI — flat i32 pointer-passing

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-10b must expose six families of host functions to plugins (network,
kv, events, jobs, api, config). Per ADR-0023 plugins target plain
`wasm32-unknown-unknown` with no WASI imports, so every capability the
plugin needs is a Lahijan-defined import. We need to pick the ABI shape
that those imports use to exchange data with the host.

WebAssembly core types are only `i32 / i64 / f32 / f64`. There are no
strings, structs, lists, or records in the type system. Every design
therefore picks ONE of these strategies:

- **Option A — Component Model / Interface Types (WASI Preview 2).**
  Rich types (records, variants, lists) described in WIT, marshalled
  via the canonical ABI. Pros: ergonomic; language-agnostic bindings
  via `wit-bindgen`. Cons: wazero's component-model support is still
  experimental (per ADR-0023); the WIT toolchain is in flux; binding
  generators are uneven; a single breaking upstream change forces every
  plugin author to recompile.
- **Option B — Flat i32 pointer-passing into the caller's linear
  memory.** Plugin writes a byte sequence into its own memory, passes
  the `(ptr, len)` pair to the host function, the host reads/writes
  that memory via `api.Memory`, and returns a status code (or
  length-written) as an i32. Pros: stable since WASM 1.0; trivially
  supported by every language toolchain; the same pattern used by WASI
  Preview 1, AssemblyScript, and most host SDKs; works with wazero v1.9
  with no experimental features. Cons: more boilerplate on both sides
  of the ABI; plugin must explicitly allocate/copy; no compile-time
  type-checking of the payload.
- **Option C — Custom ABI over shared globals.** The host exposes
  global variables the plugin reads/writes. Rejected: globals are
  per-instance, not per-call, and the encoding is no simpler than
  Option B.

## Decision

Lahijan's MVP host-function ABI is **Option B — flat i32 pointer-passing
into the caller's linear memory**.

### Conventions

1. **One host module per family.** Imports are names-spaced:
   - `lahijan_network::http_request`
   - `lahijan_kv::{get, set, delete}`
   - `lahijan_events::emit`
   - `lahijan_jobs::schedule`
   - `lahijan_api::register_handler`
   - `lahijan_config::get`

2. **Strings and byte arrays** are passed as `(ptr i32, len i32)`. The
   pointer is an offset into the calling module's linear memory. The
   host reads the bytes via `api.Memory.Read(ptr, len)` and never
   retains a reference past the host call's return (memory may move on
   `memory.grow`).

3. **Return values** are a single `i32` status code:
   - `0` — success.
   - positive — host-specific success-with-length (e.g. bytes written
     into the plugin's buffer for `kv_get`).
   - `-1` — generic failure (permission denied, validation, IO).
   - other negative values — host-specific error codes, documented per
     function. The canonical mapping lives in
     `internal/app/lahijan/wasm/hostfuncs/codes.go`.

4. **Caller identity** is propagated through the call `context.Context`:
   `runtime.Runtime.Call` injects the calling `Instance`'s pluginID
   under `hostfuncs.PluginIDKey`. Host functions fail closed when the
   key is absent.

5. **Outbound buffers** (when the host writes back to plugin memory)
   use the convention `(buf_ptr i32, buf_cap i32) -> bytes_written i32`.
   The plugin allocates the buffer; the host truncates to capacity and
   returns the number of bytes actually written. The plugin detects
   truncation by comparing `bytes_written` to its expected size.

6. **Time** is passed as `i64` Unix milliseconds (UTC). The host
   enforces a maximum `run_at` offset from "now" (per-plugin) to
   prevent a misbehaving plugin from queuing work years in the future.

7. **Permission enforcement** happens before any side effect. Every
   host function calls `Enforcer.Allowed(ctx, pluginID, slug)` first
   and returns `-1` (denied) when the slug is not granted. The denial
   is logged at `slog.Warn` with the plugin id + slug so operators can
   see misbehaving plugins.

8. **Tracing** — every host call opens an OpenTelemetry span named
   `wasm.host.<module>.<func>` carrying `plugin.id`, `permission.slug`,
   and the result code. This is the WS-10b DoD item "every host function
   call is observable in OTel traces".

### Why not Component Model now

We expect to revisit this when wazero's component-model support moves
out of experimental. The migration will be purely additive: a new
`lahijan_v2` import namespace layered on top of the flat surface, with
old plugins continuing to work unchanged. ADR-0023's "future migration
is purely additive" rationale applies here too.

## Consequences

- **Positive:** no experimental wazero features; works with every WASM
  toolchain; smallest possible attack surface; permission check is the
  very first instruction in every host function.
- **Positive:** the ABI is identical in shape to WASI Preview 1's, so
  plugin authors who have seen any host-function SDK recognise the
  pattern immediately.
- **Negative:** plugin authors must explicitly allocate/copy strings;
  there is no compile-time type-checking of payload shape.
- **Negative:** the host must read/write the caller's memory carefully;
  a bug here is a memory-corruption vulnerability.

## Compliance

- All host modules are registered in
  `internal/app/lahijan/wasm/hostfuncs/registrar.go` via one
  `HostFunctionsRegistrar` passed to `runtime.Config.HostFunctions`.
- Status code constants live in
  `internal/app/lahijan/wasm/hostfuncs/codes.go` and are documented in
  `docs/architecture/plugins.md` (WS-10c).
- Every host function opens an OTel span via
  `hostfuncs.startSpan(ctx, module, fn, pluginID)`.
- Every host function calls `enforcer.Allowed` before performing any
  side effect.
- An integration test (`hostfuncs_integration_test.go`) round-trips a
  hand-encoded WASM module through each host function and asserts the
  result code.

## References

- ADR-0012 (WASM plugin system in MVP scope)
- ADR-0023 (WASM target — plain `wasm32-unknown-unknown`)
- WS-10b (WASM Host Functions + Event Bus)
- [WebAssembly 1.0 spec — reference types](https://www.w3.org/TR/wasm-core-1/)
- [wazero HostFunctionBuilder](https://pkg.go.dev/github.com/tetratelabs/wazero#HostFunctionBuilder)
