# ADR-0039: WASI plugin runtime mode

- **Status:** Accepted
- **Date:** 2026-07-25
- **Deciders:** maintainer
- **Related:** ADR-0023 (extends, does not supersede)

## Context

ADR-0023 chose plain `wasm32-unknown-unknown` (no WASI) for the MVP plugin
target. The rationale: the manifest permission list IS the full import list,
no ambient surface to audit, every WASM-producing language can target it.
ADR-0023 explicitly said "WASI Preview 2 is a future candidate once wazero's
component-model support stabilises."

The need is now real: some plugin use-cases require capabilities that
plain-WASM cannot provide — filesystem access (a log-rotator plugin that
writes structured logs to disk), stdlib assumptions (a Rust crate compiled
to `wasm32-wasi` that internally calls `fd_write`), or existing third-party
WASI modules an operator wants to reuse.

The question: can WASI plugins run under the **same** Android-style
permission model (pillar 5), or must they get a carte-blanche `*` grant?

Options considered:

- **Option A — Filtered WASI importer + dedicated slugs.** Register only the
  WASI Preview 1 functions implied by the plugin's granted slugs. Map each
  WASI capability to a Lahijan permission slug:
  `wasi.fs.preopen:<path>:rw`, `wasi.env:VAR_NAME`, `wasi.clock`,
  `wasi.random`, `wasi.exit`. Filesystem via wazero's `FSConfig`
  (per-dir preopens, read-only vs read-write). Environment via
  `WithEnvVars` (only injected values, never host's env). Pros: preserves
  pillar 5; admin sees exactly what the plugin can touch; fail-closed
  (ungranted WASI import → instantiation trap). Cons: custom importer
  builder; a three-segment qualifier (`:rw`) extends the enforcer parser.
- **Option B — `*` god-mode only.** Any WASI plugin declares `*` and gets
  the full `wasi_snapshot_preview1.InstantiateSnapshotPreview1` surface.
  Pros: simplest; no new slugs. Cons: abandons pillar 5 for WASI; an admin
  cannot grant filesystem-without-env or vice versa.
- **Option C — Coarse capabilities.** Use wazero's FS/env knobs without
  dedicated slugs; admin approves "filesystem access" as one blob. Cons:
  less granular than the existing model; inconsistent with `kv.read:ns`.

Key insight: **WASI Preview 1 has no sockets.** Outbound HTTP still goes
through `lahijan_network.http_request` and inherits the existing
`network.outbound` permission + URL allowlist. The riskiest ambient surface
(network) was never available via WASI — so fine-grained permissions remain
coherent.

## Decision

**Option A — filtered WASI importer + dedicated slugs**, with `*` as an
explicit escape hatch for fully-trusted plugins.

### WASI permission slug catalog

| Slug | Gates |
|------|-------|
| `wasi.fs.preopen:<guest-path>:ro` | Read-only mount of `<fs_root>/<plugin-slug>/<host_subdir>` at `<guest-path>`. |
| `wasi.fs.preopen:<guest-path>:rw` | Read-write mount of the same. |
| `wasi.env:<VAR_NAME>` | Surface the named env var (value injected by admin via config). |
| `wasi.clock` | `clock_time_get` / `clock_res_get`. |
| `wasi.random` | `random_get` (default-grant; low risk). |
| `wasi.exit` | `proc_exit`. |
| `*` | Full `wasi_snapshot_preview1` surface (god-mode; UI shows a loud warning). |

### Manifest shape

```yaml
runtime: wasi      # default "none" = plain wasm32-unknown-unknown
wasi:
  preopens:
    - guestPath: /data
      hostSubdir: data
      mode: rw
  env:
    - API_ENDPOINT
  clock: true
  random: true
```

### Runtime selection

`runtime.Runtime.Instantiate` reads `manifest.Runtime`. `none` → existing
plain-WASM path. `wasi` → the filtered importer builds the host module from
grants, wires it alongside the existing Lahijan host modules, and
instantiates. Network stays on `lahijan_network` regardless.

### Filesystem sandbox

All preopens are under `<conf.wasm.wasi.fs_root>/<plugin-slug>/`. A plugin
cannot reach another plugin's dir or the host filesystem. wazero's path
sanitisation + our prefix mount enforce this.

## Consequences

- **Positive:** WASI plugins run under the same permission model as
  plain-WASM plugins; pillar 5 is preserved.
- **Positive:** fail-closed by construction — a WASI import the plugin
  declares but the filtered importer did not register causes instantiation
  to trap.
- **Positive:** network permissions are unaffected (WASI P1 has no sockets).
- **Negative:** a custom importer builder is additional maintenance surface.
- **Negative:** the three-segment qualifier (`wasi.fs.preopen:/data:rw`)
  extends the enforcer's slug parser.
- **Neutral:** ADR-0023's "WASI deferred" stance is now partially fulfilled.
  Preview 2 / Component Model remains future. This ADR extends ADR-0023, it
  does not supersede it — plain-WASM plugins continue to work unchanged.

## Compliance

- `internal/app/lahijan/wasm/permission/permission.go` declares the WASI
  slugs in `allCapabilities`.
- `internal/app/lahijan/wasm/manifest/manifest.go` declares `Runtime` +
  `Wasi` fields; `Validate` enforces the `wasi:` block when `runtime: wasi`.
- `internal/app/lahijan/wasm/wasiimporter/` builds the filtered host module
  from grants.
- `runtime.Runtime.Instantiate` branches on `manifest.Runtime`.
- A test asserts: WASI import not covered by a grant → instantiation fails.
- A test asserts: filesystem confined to `<fs_root>/<plugin-slug>/`.

## References

- WS-10e (WASI Plugin Runtime Mode)
- ADR-0012 (WASM plugin system in MVP scope)
- ADR-0023 (WASM target — plain `wasm32-unknown-unknown`; this ADR extends)
- ADR-0024 (Host-function ABI)
- [wazero WASI Preview 1](https://pkg.go.dev/github.com/tetratelabs/wazero#section-sections-wasi)
- [WASI Preview 1](https://github.com/WebAssembly/WASI/blob/main/legacy/preview1/docs.md)
