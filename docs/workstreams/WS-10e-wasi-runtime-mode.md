# WS-10e · WASI Plugin Runtime Mode

```
Status: in-progress
Phase: 2
Depends on: WS-10a, WS-10c
Unblocks: —
```

## Goal

Plugins compiled to `wasm32-wasi` (Rust `wasm32-wasi`, TinyGo `-target wasi`,
Zig, AssemblyScript-with-WASI, …) run inside Lahijan under the **same
Android-style permission model** as plain-WASM plugins, instead of being
rejected at instantiation. WASI capabilities (preopened directories,
environment variables, clock, randomness, exit) are mapped to dedicated
Lahijan permission slugs that the admin approves per-plugin, exactly like
`kv.read` or `network.outbound`. A `*` escape hatch exists for fully-trusted
plugins. This closes the biggest gap of ADR-0023's plain-WASM-only stance
without abandoning pillar 5 (Android-style permissions).

## Scope

**In scope:**

### Manifest extension

- Add a top-level `runtime` field to `lahijan.manifest.yaml` (default `none` =
  plain `wasm32-unknown-unknown`, the existing path). Value `wasi` selects
  the WASI runtime mode.
- Add a `wasi:` capability block declaring what the plugin needs:

  ```yaml
  runtime: wasi
  wasi:
    preopens:
      - path: /data                    # guest path the plugin sees
        hostSubdir: data               # under <fs_root>/<plugin-slug>/
        mode: rw                       # ro | rw
    env:
      - API_ENDPOINT                   # names only; values injected by admin
    clock: true                        # wasi.clock
    random: true                       # wasi.random (default true; safe)
  ```

- Each `wasi:` entry maps to a permission slug the admin approves (see
  below). The manifest validator (`internal/app/lahijan/wasm/manifest`)
  enforces shape; the installer surfaces the slug list to the admin UI
  exactly as it does for `kv.read:*` today.

### New permission slugs

In `internal/app/lahijan/wasm/permission/permission.go`:

- `wasi.fs.preopen:<guest-path>` — mount a directory at the given guest path.
  Read-only vs read-write is encoded in a qualifier suffix (`:ro` / `:rw`);
  default `ro`.
- `wasi.env:<NAME>` — surface the named env var to the plugin (value injected
  by admin via the config endpoint; never the host's actual env).
- `wasi.clock` — allow `clock_time_get` / `clock_res_get`.
- `wasi.random` — allow `random_get` (default-grant; low risk).
- `wasi.exit` — allow `proc_exit`.
- `*` — the god-mode escape hatch: registers the full
  `wasi_snapshot_preview1.InstantiateSnapshotPreview1` surface with no
  filtering. Admin must explicitly grant `*`; the install UI shows a loud
  warning.

### Filtered WASI importer

- New package `internal/app/lahijan/wasm/wasiimporter/` that builds a wazero
  host module registering **only** the WASI Preview 1 functions implied by the
  plugin's granted slugs.
- **Filesystem:** `wazero.Sys.NewFSConfig().WithDirMount(hostPath, guestPath)`
  per granted `wasi.fs.preopen:*`. `WithReadOnlyMount` for `:ro`. The
  `hostPath` is always `<fs_root>/<plugin-slug>/<hostSubdir>`; a plugin can
  never reach outside its own `<plugin-slug>/` subdir.
- **Environment:** `wazero.NewModuleConfig().WithEnvVars(k, v)` for each
  granted `wasi.env:*`; values read from the admin-set plugin config
  (encrypted at rest when `secret: true`, same path as `config.read`).
- **Functions:** register `clock_time_get` / `clock_res_get` only when
  `wasi.clock` granted; `random_get` only when `wasi.random` granted;
  `proc_exit` only when `wasi.exit` granted. A WASI import the plugin
  declares but the filtered importer did not register causes instantiation to
  fail closed — exactly the desired behaviour.
- `args_get` / `args_sizes_get` / `environ_get` / `environ_sizes_get` are
  always wired but scoped to the injected values (never the host's argv/env).

### Runtime selection

- `runtime.Runtime.Instantiate` reads the manifest's `runtime` field. `none`
  → existing plain-WASM path (WS-10a). `wasi` → build the filtered importer
  from the plugin's grants, wire it alongside the existing Lahijan host
  modules (network / kv / events / jobs / api / config), and instantiate.
- WASI plugins may still call every Lahijan host function; they declare the
  matching permission as usual. **Network** in particular stays gated through
  `lahijan_network` (WASI Preview 1 has no sockets), so a WASI plugin's
  outbound HTTP still goes through `network.outbound` and its URL allowlist.

### Sample + docs

- New sample `examples/plugins/wasi-log-rotator/` (TinyGo `-target wasi` or
  Rust): opens a file under its preopened `/data`, appends a line per
  subscribed event. Declares `wasi.fs.preopen:/data:rw`,
  `events.listen:compute.instance.*`, writes to the file via WASI `fd_write`.
- `docs/architecture/plugins.md`: new section **"WASI plugins"** covering the
  manifest `runtime: wasi` block, the permission slug reference, the
  filesystem sandbox layout, and the `*` escape-hatch warning.
- ADR-0039 written and Accepted.

**Out of scope:**

- WASI Preview 2 / Component Model — still future per ADR-0023; this WS
  explicitly uses Preview 1 (wazero's stable `wasi_snapshot_preview1`).
- `wasi-sockets` / outbound networking via WASI — not in wazero; networking
  stays on `lahijan_network`.
- A Go SDK for WASI mode — if WS-10d is complete, TinyGo WASI plugins may use
  `sdk-go` as-is; otherwise raw WASI + raw Lahijan imports work. No SDK work
  here.
- Changing the plain-WASM path or the existing permission catalog (additive
  only).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0012-wasm-full-mvp.md`
- `docs/adr/0023-wasm-target-wasi-preview2.md` (the plain-WASM decision this
  WS extends — NOT supersedes)
- `docs/adr/0024-wasm-host-function-abi.md`
- `docs/workstreams/WS-10a-wasm-runtime-permissions.md` (the runtime +
  enforcer being extended)
- `docs/workstreams/WS-10c-wasm-sample-plugins-marketplace.md` (sample +
  install flow this WS mirrors)
- `docs/architecture/plugins.md`
- `docs/architecture/conventions.md#security`
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- [wazero WASI Preview 1 docs](https://pkg.go.dev/github.com/tetratelabs/wazero#section-sections-wasi)

## Deliverables

- `runtime` + `wasi:` manifest fields; validator updates
- WASI permission slugs in the catalog + enforcer support
- `internal/app/lahijan/wasm/wasiimporter/` filtered importer package
- Runtime instantiation branch for `runtime: wasi`
- `conf.wasm.wasi.fs_root` config key (operator-set sandbox root)
- `wasi-log-rotator` sample plugin + marketplace entry
- "WASI plugins" section in `docs/architecture/plugins.md`
- ADR-0039 written and Accepted

## Definition of Done

- [ ] a manifest with `runtime: wasi` + a `wasi:` block parses, validates,
      and surfaces the derived slugs to the admin install flow
- [ ] a WASI plugin whose declared imports exceed its granted WASI slugs
      **fails to instantiate** (fail-closed test)
- [ ] a WASI plugin with `wasi.fs.preopen:/data:rw` granted can create +
      write a file under `<fs_root>/<plugin-slug>/data/`; a plugin without
      that grant cannot (filesystem isolation test)
- [ ] a WASI plugin cannot read env vars the admin did not inject via
      `wasi.env:*` (env scoping test)
- [ ] a WASI plugin can still call `lahijan_network.http_request`, `kv.*`,
      `events.*` when granted those slugs (Lahijan host funcs work alongside
      WASI)
- [ ] a WASI plugin with `*` granted instantiates with the full
      `wasi_snapshot_preview1` surface; the install UI shows a loud warning
      for `*`
- [ ] filesystem access is confined to `<fs_root>/<plugin-slug>/` — a test
      confirms a plugin cannot escape via `..` (wazero path sanitisation +
      our prefix mount)
- [ ] every install/grant/revoke/enable/disable/delete emits an audit event
      (parity with WS-10a)
- [ ] `wasi-log-rotator` sample builds (`tinygo build -target wasi` or
      `cargo build --target wasm32-wasi`) and its Makefile `verify` target
      asserts the module declares the expected WASI imports
- [ ] `docs/architecture/plugins.md` "WASI plugins" section is accurate
- [ ] ADR-0039 written, status Accepted, listed in `docs/adr/README.md`;
      cross-linked from ADR-0023
- [ ] `make lint test` green

## Open questions

- **Filesystem sandbox root.** Proposal: a single operator-configured dir
  (`conf.wasm.wasi.fs_root`, default `/var/lib/lahijan/wasi-fs/`); each
  plugin gets `<fs_root>/<plugin-slug>/`. Alternative: per-plugin
  configurable root. Proposal is simpler and uniformly safe; ADR-0039
  confirms.
- **Read-only vs read-write encoding.** Recommended: slug suffix `:ro` /
  `:rw` on `wasi.fs.preopen:<path>:<mode>` (e.g. `wasi.fs.preopen:/data:rw`).
  The enforcer's qualifier parser already handles `:`; a three-segment
  qualifier is a small extension. ADR confirms vs. the alternative (separate
  `wasi.fs.read` / `wasi.fs.write` slugs).
- **Should `wasi.random` and `wasi.clock` be default-granted?** `random_get`
  and `clock_time_get` are low-risk and many libraries assume them. Proposal:
  default-grant both unless the manifest opts out; admin can still revoke.
  ADR confirms.

## Notes

- This WS **extends** ADR-0023 rather than superseding it. ADR-0023 said
  "WASI is a future candidate"; this WS is that future, landed additively.
  Plain-WASM plugins continue to work unchanged. ADR-0039 records this
  relationship explicitly.
- WASI Preview 1 has no sockets — so the entire `network.outbound`
  permission story is unaffected. A WASI plugin's outbound HTTP still goes
  through `lahijan_network` and inherits the URL allowlist + per-grant
  scoping. This is why fine-grained permissions remain coherent for WASI:
  the risky ambient surface (network) was never available via WASI in the
  first place.
- The filtered importer is the security linchpin. It must register a WASI
  function **iff** the matching slug is granted, and omit it otherwise. A
  test enumerates every `wasi_snapshot_preview1` function and asserts each
  is gated by the right slug.
- ADR number 0039 is the next free after 0038 (WS-10d); the implementer
  confirms at write time.
- If WS-10d (Go SDK) is complete when this WS starts, the `wasi-log-rotator`
  sample should use `sdk-go` for the Lahijan host calls (events subscribe)
  and raw WASI for the file I/O. If WS-10d is not complete, the sample uses
  raw imports throughout. The two WSes are otherwise independent — this WS
  depends on WS-10a (runtime) + WS-10c (sample/install pattern), not on
  WS-10d.
