# Plugins: Developer Guide

This guide explains how to write, build, install, and operate a Lahijan
WASM plugin. It covers the manifest schema, the host-function reference,
the build process, the install flow, and the permission model. Read it
once end-to-end before writing your first plugin; refer back to it as
a reference thereafter.

> **Architecture context.** Plugins are the subject of ADR-0012 (the
> WASM plugin system in MVP scope), ADR-0023 (the WASM target — plain
> `wasm32-unknown-unknown`, no WASI), and ADR-0024 (the host-function
> ABI — flat i32 pointer-passing). Read those ADRs first if you want
> the *why* behind the design; this guide is the *how*.

## TL;DR

A Lahijan plugin is a TinyGo program compiled to plain WebAssembly.
The plugin ships with a manifest (`lahijan.manifest.yaml`) that
declares every permission the plugin needs. An admin installs the
plugin, approves each permission, and enables it. The runtime
(wazero) then loads the plugin and calls its exported entrypoints in
response to events, HTTP requests, or scheduled jobs. Every host
function the plugin calls is gated through the permission enforcer;
unapproved calls fail closed.

```sh
# Build
cd examples/plugins/_template && make build

# Install (admin session required)
curl -F 'package=@my-plugin-0.1.0.lahx' \
     -H 'Cookie: lahijan_session=...' \
     -H 'X-Tenant-Id: ...' \
     http://localhost:3000/api/v1/admin/plugins/upload

# Grant each declared permission
PLUGIN_ID=...
for slug in 'kv.read:cache' 'events.listen:dns.record.*'; do
  curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/permissions/$(echo $slug | sed 's/:/%3A/')/grant" \
       -H 'Cookie: lahijan_session=...'
done

# Enable
curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/enable" \
     -H 'Cookie: lahijan_session=...'
```

## Why WASM?

Three reasons drove the choice (see ADR-0012 for the full rationale):

1. **Sandboxed.** A WASM module cannot reach outside its linear memory
   unless the host explicitly grants it an import. Every import a
   plugin can call is gated through the permission enforcer; ungranted
   capabilities are unreachable.
2. **Language-agnostic.** Anything that can emit `wasm32-unknown-unknown`
   works. Today we ship TinyGo samples; Rust, AssemblyScript, and Zig
   plugins are equally valid.
3. **No CGO.** wazero is pure Go, so Lahijan's binary stays single-
   static-file. Operators don't need a libwasmer match.

## Target: plain `wasm32-unknown-unknown`

Per ADR-0023 plugins do **not** get WASI imports. The only imports a
plugin can call are the ones Lahijan itself exposes, and every one of
those is gated by the permission enforcer. The manifest permission list
**is** the full import list; the manifest is the single source of truth.

Concretely, this means:

- **No `os`, `io`, `fs`, `net`, `time` from the Go stdlib.** These
  pull in WASI imports Lahijan does not register; the runtime rejects
  instantiation.
- **No `fmt.Println`, no `log.Printf`.** Use the host's events bus
  (`events.emit`) or write to your KV namespace (`kv.write`) for
  diagnostics.
- **No allocation past the memory cap.** The runtime rejects modules
  whose declared memory max exceeds `conf.wasm.max_memory_per_plugin`.

TinyGo is the only Go compiler that emits the right shape today.
Standard `go build -target wasm` pulls in WASI by default and does
not work.

## The manifest

Every plugin ships a `lahijan.manifest.yaml` next to its `.wasm`. The
manifest is parsed + validated at upload time and stored verbatim in
`plugins.manifest_json` so the admin can see exactly what the plugin
declared.

```yaml
name: my-plugin                  # kebab-case, <= 64 chars
version: 1.0.0                   # semver MAJOR.MINOR.PATCH
description: "One-line summary."
author: "You <you@example.com>"
license: Apache-2.0
homepage: https://example.com/my-plugin

permissions:                     # required; may be empty
  - "kv.read:cache"
  - "kv.write:cache"
  - "events.listen:dns.record.*"
  - "network.outbound:hooks.slack.com"

config_schema:                   # optional
  type: object
  properties:
    webhook_url:
      type: string
      format: uri
      secret: true               # encrypted at rest; never surfaced back

entrypoints:                     # which exports the runtime may call
  - on_event
```

The schema is documented in
[`internal/app/lahijan/wasm/manifest/manifest.go`](../../internal/app/lahijan/wasm/manifest/manifest.go).
Upload-time validation enforces:

- **Name**: kebab-case, ≤ 64 chars.
- **Version**: `MAJOR.MINOR.PATCH` (+ optional pre-release/build suffix).
- **Permissions**: every slug must validate via
  `permission.Validate` (i.e. be in the capability catalog OR match a
  known wildcard shape).
- **Entrypoints**: non-empty identifiers.

### The permission catalog

Every capability a plugin can request is in
[`internal/app/lahijan/wasm/permission/permission.go`](../../internal/app/lahijan/wasm/permission/permission.go).
The current catalog:

| Slug | Gates |
|------|-------|
| `network.outbound` | Outbound HTTP via `lahijan_network.http_request`. |
| `kv.read:<ns>` | Reads from the per-plugin KV namespace `<ns>`. |
| `kv.write:<ns>` | Writes to the per-plugin KV namespace `<ns>`. |
| `events.emit` | `lahijan_events.emit`. |
| `events.listen:<topic>` | `lahijan_events.subscribe` for `<topic>` (may end with `.*`). |
| `job.schedule` | `lahijan_jobs.schedule`. |
| `api.handler.register:<path>` | `lahijan_api.register_handler` for `<path>` (or `:*` for any). |
| `config.read:<plugin-name>` | `lahijan_config.get` for the plugin's own config. |
| `compute.instance.create` | `lahijan_compute.instance_create`. |
| `compute.instance.read` | `lahijan_compute.instance_get` + `instance_list`. |
| `compute.instance.control` | `lahijan_compute.instance_set_state` (start/stop/restart). |
| `compute.instance.delete` | `lahijan_compute.instance_delete`. |
| `dns.zone.create` / `.read` / `.delete` | `lahijan_dns.zone_*`. |
| `dns.record.create` / `.read` / `.delete` | `lahijan_dns.record_*`. |
| `storage.bucket.create` / `.read` / `.delete` | `lahijan_storage.bucket_*`. |
| `wasi.fs.preopen:<path>:<ro\|rw>` | WASI filesystem preopen (WS-10e). |
| `wasi.env:<VAR>` | WASI environment variable (WS-10e). |
| `wasi.clock` / `wasi.random` / `wasi.exit` | WASI clock / randomness / exit (WS-10e). |
| `*` | WASI god-mode: full `wasi_snapshot_preview1` surface (WS-10e). |

Wildcard rules:

- `kv.read:*` matches every namespace on the read action.
- `events.listen:dns.record.*` matches every `dns.record.<verb>`.
- Scope-level wildcards (`kv.*`) are **not** supported — every action
  must be explicit.

The admin sees this exact list at install time; the install flow
rejects any slug not in the catalog.

## Host functions

Lahijan exposes six host modules. Each function follows the ABI from
ADR-0024: strings and byte arrays are passed as `(ptr i32, len i32)`
into the plugin's linear memory; the host reads/writes that memory
via `api.Memory`; the function returns a single `i32` status code.

### `lahijan_kv`

Per-plugin durable key-value store. The host injects the calling
plugin's id from the call context, so a plugin cannot address another
plugin's namespace.

```go
//go:wasm-import lahijan_kv get    func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_kv set    func(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32
//go:wasm-import lahijan_kv delete func(keyPtr, keyLen uint32) int32
```

Status codes: `0` success; `> 0` bytes written (for `get`);
`-2` permission denied; `-5` invalid argument; `-6` not found (for
`get`); `-7` buffer too small (for `get`).

### `lahijan_events`

In-process pub/sub. `emit` publishes to a topic; `subscribe` registers
a WASM export the host calls on every matching event.

```go
//go:wasm-import lahijan_events emit       func(topicPtr, topicLen, payloadPtr, payloadLen uint32) int32
//go:wasm-import lahijan_events subscribe  func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32
//go:wasm-import lahijan_events unsubscribe func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32
```

The standard event registry lives in
[`internal/app/lahijan/wasm/eventbus/events.go`](../../internal/app/lahijan/wasm/eventbus/events.go).
Plugins may also emit dynamic topics (`my_plugin.tick`) for
cross-instance coordination.

### `lahijan_network`

Outbound HTTP. The host applies a process-wide URL allowlist
(`conf.wasm.network_allowed_url_globs`) and a default timeout
(`conf.wasm.network_timeout_ms`). Per-grant allowlists are a Phase 7
candidate.

The 12-param ABI (ADR-0038) returns the full response (status code +
headers + body) via two caller-supplied buffers. The SDK handles buffer
sizing and retry automatically; raw authors must loop on
`StatusBufferTooSmall`.

```
http_request(method_ptr, method_len,
             url_ptr, url_len,
             req_headers_ptr, req_headers_len,
             req_body_ptr, req_body_len,
             resp_hdr_buf_ptr, resp_hdr_buf_cap,
             resp_body_buf_ptr, resp_body_buf_cap) -> http_status_or_error
```

Each response buffer is written as a 4-byte LE uint32 length prefix
followed by the data:
- `resp_hdr_buf`: `[LE uint32 json_len][headers JSON map]`
- `resp_body_buf`: `[LE uint32 body_len][body bytes]`

When both response buffer caps are 0 (the backward-compatible path),
the host returns only the HTTP status code and writes no response data.

Return value: HTTP status code (100–599) on success, or a negative
`StatusXxx` code on failure.

### `lahijan_jobs`

Schedule a future invocation of one of the plugin's own exports. The
host enqueues a River job; the job fires at the requested `runAt` and
calls the named export with the supplied args.

```go
//go:wasm-import lahijan_jobs schedule func(
//   namePtr,  nameLen  uint32,    // export name (e.g. "on_tick")
//   argsPtr,  argsLen  uint32,    // JSON-encoded args
//   runAtMs   int64,              // Unix-millis when the job fires
//) int32                              // returns the River job id (> 0) or a negative code
```

`runAt` is capped at `conf.wasm.max_run_at_offset_ms` in the future so
a misbehaving plugin cannot queue work years out.

### `lahijan_api`

Register an HTTP handler under
`/api/v1/plugins/<plugin-name>/<path>`. The handler is a WASM export
the host calls on each matching request.

```go
//go:wasm-import lahijan_api register_handler   func(methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen uint32) int32
//go:wasm-import lahijan_api unregister_handler func(methodPtr, methodLen, pathPtr, pathLen uint32) int32
```

Paths must start with `/` and must not contain `..` or `//`. The
runtime mounts under the plugin's own prefix; attempts to escape are
rejected.

### `lahijan_config`

Read admin-set per-plugin config values. Values flagged `secret: true`
in the manifest's `config_schema` are encrypted at rest and surface as
`NotFound` to the plugin (the admin UI shows them; the plugin never
sees the plaintext).

```go
//go:wasm-import lahijan_config get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
```

### Status code table

| Code | Meaning |
|------|---------|
| `0` | Success (generic). |
| `> 0` | Success-with-length (bytes written). |
| `-1` | Generic failure (validation, IO, unknown). |
| `-2` | Permission denied (the enforcer rejected the call). |
| `-3` | Feature unavailable (the host module was wired with nil deps). |
| `-4` | Invalid memory (`(ptr, len)` outside the caller's linear memory). |
| `-5` | Invalid argument (string too long, missing terminator, ...). |
| `-6` | Not found. |
| `-7` | Buffer too small. |
| `-8` | Upstream error (HTTP 5xx, provider unreachable). |
| `<= -100` | Reserved for per-function error codes. |

The canonical constants live in
[`internal/app/lahijan/wasm/hostfuncs/codes.go`](../../internal/app/lahijan/wasm/hostfuncs/codes.go).

### `lahijan_compute` (WS-10f)

Manage compute instances (containers + VMs). Every call delegates to the
compute service, which enforces tenant scoping, billing, and audit.

Each function takes `(args_ptr, args_len, buf_ptr, buf_cap)` — JSON args
in, JSON result out. Returns bytes-written (>0) or a negative code.

| Function | Args (JSON) | Result (JSON) | Permission |
|----------|-------------|---------------|------------|
| `instance_create` | `{"name","type","image_alias","profiles","config"}` | instance object | `compute.instance.create` |
| `instance_get` | `{"id":"<uuid>"}` | instance object | `compute.instance.read` |
| `instance_list` | `{"limit":50,"offset":0}` | `[instance,...]` | `compute.instance.read` |
| `instance_set_state` | `{"id","action":"start\|stop\|restart\|freeze\|unfreeze","force":false,"timeout_secs":0}` | instance object | `compute.instance.control` |
| `instance_delete` | `{"id":"<uuid>"}` | `{"deleted":true,"id":"..."}` | `compute.instance.delete` |

### `lahijan_dns` (WS-10f)

Manage DNS zones + records.

| Function | Args (JSON) | Result (JSON) | Permission |
|----------|-------------|---------------|------------|
| `zone_create` | `{"name","description","kind"}` | zone object | `dns.zone.create` |
| `zone_get` | `{"id"}` | zone object | `dns.zone.read` |
| `zone_list` | `{"limit","offset"}` | `[zone,...]` | `dns.zone.read` |
| `zone_delete` | `{"id"}` | `{"deleted":true}` | `dns.zone.delete` |
| `record_create` | `{"zone_id","name","type","content","ttl"}` | record object | `dns.record.create` |
| `record_list` | `{"zone_id","limit","offset"}` | `[record,...]` | `dns.record.read` |
| `record_delete` | `{"zone_id","record_id"}` | `{"deleted":true}` | `dns.record.delete` |

### `lahijan_storage` (WS-10f)

Manage S3 buckets.

| Function | Args (JSON) | Result (JSON) | Permission |
|----------|-------------|---------------|------------|
| `bucket_create` | `{"slug","label","description","quota_bytes","quota_objects"}` | bucket object | `storage.bucket.create` |
| `bucket_get` | `{"id"}` | bucket object | `storage.bucket.read` |
| `bucket_list` | `{"limit","offset"}` | `[bucket,...]` | `storage.bucket.read` |
| `bucket_delete` | `{"id"}` | `{"deleted":true}` | `storage.bucket.delete` |

## Build process

The only supported build path today is TinyGo. Install `tinygo >= 0.32`
from https://tinygo.org/getting-started/install/, then:

```sh
cd my-plugin
tinygo build -target wasm -o plugin.wasm main.go
```

The `_template/` directory ships a Makefile wrapper:

```sh
cd examples/plugins/_template
make build      # -> plugin.wasm
make verify     # build + assert no WASI imports
make clean
```

`make verify` runs `strings` on the produced module and rejects it if
it imports from `wasi_snapshot_preview1` or `wasi_unstable`. This is
the cheapest way to catch a stray `fmt.Println` (which would pull in
WASI's `fd_write`) before the Lahijan runtime rejects instantiation.

## Using the Go SDK (recommended)

The Go SDK (`github.com/avestura/lahijan/sdk-go`) wraps every host
function behind idiomatic Go APIs. You write `kv.Set("k", v)` instead
of 15 lines of `unsafe.Pointer` juggling and status-code switches. The
SDK auto-retries on `BufferTooSmall`, translates status codes to typed
errors, and compiles under TinyGo to plain `wasm32-unknown-unknown`
with zero WASI imports.

### Quick start

```sh
cp -r examples/plugins/_template my-plugin
cd my-plugin
# Edit go.mod, lahijan.manifest.yaml, main.go
make build
```

### Before / after

The `slack-notifier` sample went from 136 LOC (raw imports) to ~45 LOC
(SDK). Compare:

**Before (raw imports):**
```go
//go:wasm-import lahijan_config get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_network http_request func(methodPtr, methodLen, urlPtr, urlLen, headersPtr, headersLen, bodyPtr, bodyLen, respBufPtr, respBufCap uint32) int32

func on_event(payloadPtr, payloadLen uint32) {
    payload := readMem(payloadPtr, payloadLen)
    var urlBuf [512]byte
    urlLen := configGet([]byte("webhook_url"), urlBuf[:])
    if urlLen <= 0 { return }
    webhookURL := string(urlBuf[:urlLen])
    httpPostJSON(webhookURL, renderSlackMessage(payload))
}
// + 70 lines of readMem, ptrOf, configGet, httpPostJSON helpers...
```

**After (SDK):**
```go
import (
    "github.com/avestura/lahijan/sdk-go/config"
    "github.com/avestura/lahijan/sdk-go/mem"
    "github.com/avestura/lahijan/sdk-go/network"
)

func on_event(payloadPtr, payloadLen uint32) {
    payload := mem.Read(payloadPtr, payloadLen)
    webhookURL, err := config.GetString("webhook_url")
    if err != nil { return }
    _, _ = network.PostJSON(webhookURL, renderSlackMessage(payload))
}
```

### SDK packages

| Package | Functions |
|---------|-----------|
| `sdk-go/kv` | `Get(key) ([]byte, error)`, `Set(key, val, ttlMs)`, `Delete(key)` |
| `sdk-go/config` | `Get(key) ([]byte, error)`, `GetString(key) (string, error)` |
| `sdk-go/events` | `Emit(topic, payload)`, `Subscribe(pattern, handler)`, `Unsubscribe(...)` |
| `sdk-go/network` | `Do(req) (Response, error)`, `Get(url)`, `Post(url, body)`, `PostJSON(url, body)` |
| `sdk-go/jobs` | `Schedule(exportName, args, runAtMs)` |
| `sdk-go/api` | `RegisterHandler(method, path, handler)`, `UnregisterHandler(...)` |
| `sdk-go/mem` | `Read(ptr, len) []byte`, `Ptr(b) uint32`, `Len(b) uint32` |
| `sdk-go/status` | `Code` type, sentinel errors (`ErrPermissionDenied`, ...), `FromCode(int32)` |

### Error handling

Every SDK function returns a Go `error`. Status codes from the host are
translated to typed, `errors.Is`-able sentinels:

```go
val, err := kv.Get("key")
switch {
case errors.Is(err, status.ErrNotFound):
    // key doesn't exist
case errors.Is(err, status.ErrPermissionDenied):
    // kv.read not granted
case err != nil:
    // other host error
}
```

### For non-Go authors

If you are writing a plugin in Rust, AssemblyScript, or Zig, you cannot
use this SDK. See the "Host ABI porting guide" appendix below for the
raw `//go:wasm-import` signatures and the `(ptr, len)` convention.

## Install flow

There are two ways to install a plugin:

1. **Direct upload** (admin-only). POST the `.wasm` + manifest to
   `/api/v1/admin/plugins/upload`. The installer parses + validates the
   manifest, compiles the wasm bytes under the configured memory cap,
   persists a row in `pending` status, and emits an audit event.
2. **Marketplace install** (admin-only). POST to
   `/api/v1/admin/plugins/install/{name}`. The installer fetches the
   `.wasm` + manifest from the configured marketplace index, verifies
   the sha256 pin, then runs the same persist + audit path as direct
   upload.

After install the plugin is in `pending` status. The admin must:

1. **Grant each declared permission** via
   `POST /api/v1/admin/plugins/{id}/permissions/{perm}/grant`. The
   permission slug uses `:` as the qualifier separator; URL-encode it
   as `%3A` if your client doesn't do so automatically.
2. **(Optional) Set admin config keys** via
   `POST /api/v1/admin/plugins/{id}/config`. Each value is a JSON blob;
   mark secrets with `"is_secret": true` so they're encrypted at rest.
3. **Enable** the plugin via
   `POST /api/v1/admin/plugins/{id}/enable`. The runtime may now
   instantiate it on demand.

The plugin's status transitions are:

```
pending  --enable-->  active  --disable-->  disabled
   |                       ^                     |
   +----- enable ---------+---- enable ---------+
```

Disabling preserves grants + config; enabling picks them back up.
Hard-deleting (DELETE `/api/v1/admin/plugins/{id}`) removes the row
and CASCADEs to grants, config, KV, subscriptions, and HTTP handler
mounts.

## Upgrade flow

A new version of a marketplace plugin arrives. The admin POSTs to
`/api/v1/admin/plugins/upgrade/{name}`. The upgrade flow:

1. Locate the most recent prior version (by name).
2. Semver-compare; reject same-version + downgrades.
3. Upload the new version (status=pending), preserving grants the new
   manifest still requests, dropping grants it no longer requests,
   and surfacing new permissions in the response.
4. Hard-delete the old row. CASCADE removes orphan state.

The response includes `newPermissions` — the admin must POST
`/permissions/{perm}/grant` for each before calling `/enable`.

## Removal flow

`DELETE /api/v1/admin/plugins/{id}` hard-deletes the row. CASCADE on
`plugins.id` removes:

- `plugin_permissions` (grants)
- `plugin_kv` (the plugin's namespace)
- `plugin_config` (admin-set config values)
- `plugin_event_subscriptions` (durable subscriptions)
- `plugin_http_handlers` (HTTP handler mounts)

An audit event (`plugins.delete`) is emitted before the CASCADE.

## Marketplace

The marketplace is an index of plugins + a content root. The index is
a single YAML file named `plugins-marketplace.yaml`; the content root
holds the `.wasm` + manifest for each entry. The default in-repo
marketplace lives at
[`examples/plugins/marketplace/`](../../examples/plugins/marketplace/).

Operators configure the marketplace via:

```yaml
wasm:
  marketplace:
    path: "examples/plugins/marketplace"   # local directory
    url: ""                                  # OR remote HTTP endpoint
    cacheTtlSeconds: 60
```

The admin endpoints are:

- `GET /api/v1/admin/marketplace` — list entries.
- `GET /api/v1/admin/marketplace/{name}` — single entry.
- `POST /api/v1/admin/plugins/install/{name}` — install the entry.
- `POST /api/v1/admin/plugins/upgrade/{name}` — upgrade to the entry.

The installer verifies every downloaded `.wasm` against the index's
sha256 pin. A mismatch is rejected with `422 Unprocessable Entity` and
an audit row recording the breach.

### Adding a plugin to the in-repo marketplace

1. Build the plugin's `plugin.wasm` via `tinygo build -target wasm`.
2. Copy it (and the manifest) into
   `examples/plugins/marketplace/<name>/`.
3. Compute the sha256:

   ```sh
   sha256sum examples/plugins/marketplace/<name>/plugin.wasm
   ```

4. Add an entry to
   `examples/plugins/marketplace/plugins-marketplace.yaml` with the
   matching name, version, permissions, and sha256.

## Tutorial: write your first plugin

This tutorial walks through building a plugin that listens for
`compute.instance.stopped` events and writes the instance id to KV.
It's a 30-line plugin that demonstrates the events + KV surface.

### Step 1: scaffold

```sh
cp -r examples/plugins/_template examples/plugins/instance-watcher
cd examples/plugins/instance-watcher
# Edit lahijan.manifest.yaml + main.go per the steps below.
```

### Step 2: declare permissions

```yaml
# lahijan.manifest.yaml
name: instance-watcher
version: 1.0.0
description: "Records every stopped instance id to KV."
permissions:
  - "events.listen:compute.instance.stopped"
  - "kv.write:state"
  - "kv.read:state"
entrypoints:
  - on_event
```

### Step 3: write the entrypoint

```go
// main.go
package main

import (
	"unsafe"
)

//go:wasm-import lahijan_kv set func(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32
//go:wasm-import lahijan_events subscribe func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32

//export on_init
func on_init() {
	topic := []byte("compute.instance.stopped")
	handler := []byte("on_event")
	subscribe(
		uint32(uintptr(unsafe.Pointer(&topic[0]))), uint32(len(topic)),
		uint32(uintptr(unsafe.Pointer(&handler[0]))), uint32(len(handler)),
	)
}

//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(payloadPtr))), payloadLen)
	key := []byte("last_stopped")
	set(
		uint32(uintptr(unsafe.Pointer(&key[0]))), uint32(len(key)),
		uint32(uintptr(unsafe.Pointer(&payload[0]))), uint32(len(payload)),
		0,
	)
}

func main() {}
```

### Step 4: build

```sh
make build
make verify   # assert no WASI imports
```

### Step 5: install

```sh
curl -F 'package=@my-plugin-0.1.0.lahx' \
     -H 'Cookie: lahijan_session=...' \
     -H 'X-Tenant-Id: ...' \
     http://localhost:3000/api/v1/admin/plugins/upload
# -> {"id": "...", "status": "pending", ...}
```

Grant each declared permission:

```sh
PLUGIN_ID=...
for slug in \
    'events.listen%3Acompute.instance.stopped' \
    'kv.write%3Astate' \
    'kv.read%3Astate' ; do
  curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/permissions/$slug/grant" \
       -H 'Cookie: lahijan_session=...'
done
```

Enable:

```sh
curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/enable" \
     -H 'Cookie: lahijan_session=...'
```

The plugin is now live. The next `compute.instance.stopped` event
fires `on_event`, which writes the instance id to KV. You can read
it back from the admin API (or a sibling plugin with `kv.read:state`).

## Reference

- [`internal/app/lahijan/wasm/`](../../internal/app/lahijan/wasm/) —
  the runtime, host functions, manifest parser, enforcer, installer,
  and marketplace.
- [`examples/plugins/`](../../examples/plugins/) — the three sample
  plugins + the template + the marketplace index.
- [ADR-0012](../adr/0012-wasm-full-mvp.md) — WASM plugin system in
  MVP scope.
- [ADR-0023](../adr/0023-wasm-target-wasi-preview2.md) — WASM target
  rationale.
- [ADR-0024](../adr/0024-wasm-host-function-abi.md) — host-function
  ABI.
- [WS-10a](../workstreams/WS-10a-wasm-runtime-permissions.md) — the
  runtime + permission enforcer.
- [WS-10b](../workstreams/WS-10b-wasm-host-functions-event-bus.md) —
  the host functions + event bus.
- [WS-10c](../workstreams/WS-10c-wasm-sample-plugins-marketplace.md)
  — the sample plugins + marketplace scaffolding (this WS).
