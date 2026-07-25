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

# Install (admin session required)
curl -F 'wasm=@plugin.wasm;type=application/wasm' \
     -F 'manifest=@lahijan.manifest.yaml;type=text/yaml' \
     -H 'Cookie: lahijan_session=...' \
     http://localhost:3000/api/v1/admin/plugins/upload
```
