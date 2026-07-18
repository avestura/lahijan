# `<your-plugin-name>`

A starter template for a Lahijan WASM plugin. Copy this directory, rename
the package, edit the manifest, and run `make build` to produce a `.wasm`
module ready to upload to Lahijan.

## Prerequisites

- [`tinygo` >= 0.32](https://tinygo.org/getting-started/install/) — the only
  compiler that reliably emits plain `wasm32-unknown-unknown` modules with
  no WASI imports (per ADR-0023). Standard `go build` does NOT work because
  it pulls in WASI by default.
- `make` (GNU Make or any compatible).

## Layout

```
_template/
├── README.md                      # this file
├── lahijan.manifest.yaml          # plugin declaration (permissions, entrypoints)
├── main.go                        # plugin entrypoint
├── go.mod                         # tinygo-compatible go.mod
└── Makefile                       # build + clean targets
```

## Build

```sh
make build
# -> plugin.wasm in this directory
```

The Makefile invokes:

```sh
tinygo build -target wasm -o plugin.wasm main.go
```

`-target wasm` is the TinyGo convention for `wasm32-unknown-unknown` (no
WASI). The resulting module has the imports your `main.go` declares (e.g.
`lahijan_kv.set`) and zero ambient surface.

## Install

Upload the `.wasm` + `lahijan.manifest.yaml` via the admin API:

```sh
curl -F 'wasm=@plugin.wasm;type=application/wasm' \
     -F 'manifest=@lahijan.manifest.yaml;type=text/yaml' \
     -H 'Cookie: lahijan_session=<your-admin-session>' \
     -H 'X-Tenant-Id: <tenant-uuid>' \
     http://localhost:3000/api/v1/admin/plugins/upload
```

The plugin lands in `pending` status. Grant each permission the manifest
declared, then call `/enable` to flip it to `active`.

See `docs/architecture/plugins.md` in the Lahijan repo for the full
developer guide.
