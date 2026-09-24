# Plugins

Lahijan plugins are WebAssembly modules that extend the platform with
custom logic — event-driven automation, HTTP endpoints, scheduled jobs,
and infrastructure management — all sandboxed behind an Android-style
permission system.

## Choose your mode

| Mode | Target | Use when |
|------|--------|----------|
| **Plain WASM** (`wasm32-unknown-unknown`) | Default | You need events, KV, outbound HTTP, or infrastructure control without filesystem access. Compiled via TinyGo. |
| **WASI** (`wasm32-wasi`) | `runtime: wasi` in manifest | You need filesystem access, stdlib I/O, or are reusing an existing WASI-compiled module. Compiled via TinyGo (`-target wasi`), Rust (`wasm32-wasi`), Zig, etc. |

Both modes share the same permission model and the same Lahijan host
functions (KV, events, network, compute, DNS, storage, jobs, config).

## Guides

- [Writing Plugins](./writing-plugins.md) — comprehensive guide for both modes
- [SDK Reference](./sdk-reference.md) — full Go SDK API reference
- [Plugin Architecture](../architecture/plugins.md) — host-function ABI, manifest schema, permission catalog

## Quick start

```sh
# Scaffold from the template
cp -r examples/plugins/_template my-plugin
cd my-plugin

# Edit go.mod, lahijan.manifest.yaml, main.go
# Build
make build
make verify   # assert no WASI imports (plain-WASM mode)
make package  # -> <name>-<version>.lahx (manifest + module, validated)

# Install (admin session required): upload the .lahx in the dashboard
# (Admin -> Plugins -> Upload extension), or:
curl -F 'package=@my-plugin-0.1.0.lahx' \
     -H 'Cookie: lahijan_session=...' \
     http://localhost:3000/api/v1/admin/plugins/upload
```

## Extension packages (`.lahx`)

Plugins are distributed and uploaded as one file: a **`.lahx` extension
package**. It is a ZIP archive with exactly two entries at its root:

| Entry | Content |
|---|---|
| `lahijan.manifest.yaml` | the plugin manifest |
| `plugin.wasm` | the compiled WebAssembly module |

Build one with `make package` (runs `go run ./cmd/lahx pack <dir>`, which
validates the manifest first) or with any ZIP tool:
`zip -j my-plugin.lahx lahijan.manifest.yaml plugin.wasm`. Inspect one with
`go run ./cmd/lahx inspect my-plugin.lahx`.

The server rejects packages with other entries, nested paths, duplicate or
encrypted entries, or entries beyond the size caps (`wasm.maxModuleSize` for
the module, 64 KiB for the manifest). Marketplace entries may ship a
`plugin.lahx` too; it takes precedence over loose files.
