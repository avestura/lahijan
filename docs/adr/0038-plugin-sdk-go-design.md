# ADR-0038: Plugin SDK (Go) design

- **Status:** Accepted
- **Date:** 2026-07-25
- **Deciders:** maintainer

## Context

WS-10a/b/c shipped the WASM plugin runtime, host functions, and sample
plugins. Every plugin author must re-derive ~60 lines of ABI plumbing:
`//go:wasm-import` declarations, `unsafe.Pointer`/`uintptr` memory juggling,
`(ptr, len)` pairs, `BufferTooSmall` retry loops, and status-code switch
statements (see `examples/plugins/slack-notifier/main.go` — 136 lines of
which ~60 are plumbing).

Additionally, `lahijan_network.http_request` has a known gap: it returns only
the HTTP status code and drops the response body + headers
(`network.go:172` TODO). Any useful SDK needs `Network.Request() -> Response`
with the full body.

Three design questions needed answers:

1. **Module location.** Where does the SDK live so external authors `go get`
   it without pulling the backend?
2. **Response-buffer ABI.** How does `http_request` return the response body
   + headers? Inline params or a prior `set_response_buffers` call?
3. **Allocator + error model.** How does the SDK manage linear memory for
   host-call round-trips, and how does it surface status codes as Go errors?

Options considered for module location:

- **Option A — `sdk-go/` at repo root as its own Go module.** Pros: clean
  import path `github.com/avestura/lahijan/sdk-go`; zero backend deps;
  matches Lahijan's existing convention of top-level single-purpose dirs
  (`web/`, `website/`, `examples/`); project-layout-compatible in spirit
  (public, clearly named, independently `go get`-able). Cons: Lahijan uses
  no `/pkg` dir, which `project-layout.md` champions for public libs — but
  Lahijan already diverges (uses `internal/` + purpose-named dirs).
- **Option B — `pkg/sdk-go/`.** Pros: follows project-layout.md literally.
  Cons: ugly import path; no `/pkg` precedent in Lahijan; creates a new
  top-level dir for one consumer.
- **Option C — inside `examples/plugins/`.** Cons: `examples/` is not an
  import path third parties should depend on.

Options considered for response-buffer ABI:

- **Option A — Inline buffers.** Add `respHdrBuf(ptr,cap)` +
  `respBodyBuf(ptr,cap)` to `http_request` (8→12 params). Stateless; the host
  writes into the caller-supplied buffers and returns the HTTP status code or
  `StatusBufferTooSmall`. Pros: simplest reasoning; matches the 10-param
  shape already documented in `plugins.md`; the SDK hides the retry.
  Cons: slightly larger call signature.
- **Option B — Slot-based.** Plugin calls `set_response_buffers(...)` once,
  then `http_request` reuses those slots. Pros: fewer params on the hot
  path. Cons: stateful; harder for raw authors; the host must track per-
  instance buffer addresses.

## Decision

1. **Module location: Option A — `sdk-go/` at repo root** as a standalone Go
   module (`github.com/avestura/lahijan/sdk-go`). Internal packages follow
   project-layout's naming rules (package name = directory name; no
   `util`/`common`/`helpers`). The module has zero backend imports and
   compiles under TinyGo to `wasm32-unknown-unknown` with no WASI imports.

2. **Response-buffer ABI: Option A — inline.** `http_request` grows from 8
   to 12 i32 params:

   ```
   http_request(method_ptr, method_len, url_ptr, url_len,
                reqHeaders_ptr, reqHeaders_len, reqBody_ptr, reqBody_len,
                respHdrBuf_ptr, respHdrBuf_cap,
                respBodyBuf_ptr, respBodyBuf_cap) -> int32
   ```

   The host writes the response headers (JSON map) + body into the
   caller-supplied buffers and returns the HTTP status code. If either
   buffer is too small the host returns `StatusBufferTooSmall` and writes
   nothing. The existing 8-param shape is a strict subset (pass `0,0,0,0`
   for the new buffers → old behaviour).

3. **Error model: typed sentinel errors** mapped 1:1 to
   `hostfuncs/codes.go`, all `errors.Is`-able. A `Code` type wraps the raw
   int32; `FromCode(int32) error` is the single translation point.
   `BufferTooSmall` retries are hidden inside the SDK (the SDK grows its
   buffer and re-issues the call).

4. **Memory helpers: `unsafe.Pointer`/`uintptr` conversions** — the same
   pattern the existing samples use. The SDK's `mem` package owns `Ptr`,
   `Read`, and `Len`; no separate allocator is needed because TinyGo's GC
   manages linear memory and slices survive for the duration of a host call.

## Consequences

- **Positive:** plugin authors write `kv.Set("k", v)` instead of 15 lines of
  pointer juggling. The SDK is independently versioned; a host-side ABI
  bump does not force the backend to upgrade.
- **Positive:** the network response fix is small and backward-compatible
  (existing 8-param callers pass `0,0,0,0`).
- **Negative:** the SDK is another Go module to maintain; version skew
  between SDK and host ABI is possible (mitigated by the stable status-code
  table and the additive ABI change).
- **Neutral:** the SDK uses `encoding/json` for network headers, which adds
  binary size under TinyGo. This is acceptable (plugins already need JSON)
  and is dead-code-eliminated when the network package is unused.

## Compliance

- `sdk-go/go.mod` declares module `github.com/avestura/lahijan/sdk-go` with
  no `require` directives (zero external deps).
- `sdk-go/status/status.go` defines `Code`, the sentinel errors, and
  `FromCode`. Every error in `codes.go` has a matching sentinel.
- `sdk-go/mem/mem.go` defines `Ptr`, `Read`, `Len`.
- Each host-function family (`kv`, `events`, `network`, `jobs`, `api`,
  `config`) declares its own `//go:wasm-import` directives and provides
  idiomatic wrappers.
- `internal/app/lahijan/wasm/hostfuncs/network.go` registers `http_request`
  with 12 i32 params and writes response headers + body into the buffers.
- `hostfuncs_integration_test.go` round-trips a full HTTP response
  (status + headers + body) end-to-end.
- Sample plugins (`slack-notifier`, `autoscaler-stub`, `dns-record-hook`,
  `_template`) `require github.com/avestura/lahijan/sdk-go` and contain no
  `unsafe.Pointer` or `//go:wasm-import` declarations.

## References

- WS-10d (Plugin SDK + Host Network Response Fix)
- ADR-0012 (WASM plugin system in MVP scope)
- ADR-0023 (WASM target — plain `wasm32-unknown-unknown`)
- ADR-0024 (Host-function ABI — flat i32 pointer-passing)
- [TinyGo `//go:wasm-import` directive](https://tinygo.org/docs/guides/webassembly/)
