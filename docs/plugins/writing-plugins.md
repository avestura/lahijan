# Writing Plugins

This guide walks through writing, building, installing, and operating a
Lahijan plugin in both plain-WASM and WASI modes. It covers the Go SDK
(recommended for Go authors) and the raw host-function ABI (for other
languages).

> **Architecture context.** Read [Plugin Architecture](../architecture/plugins.md)
> for the full host-function reference, manifest schema, and permission
> catalog. This guide is the *how*; that page is the *what*.

## Prerequisites

- **TinyGo** >= 0.32 (for Go plugins): https://tinygo.org/getting-started/install/
- **Lahijan SDK** (for Go plugins): `github.com/avestura/lahijan/sdk-go`
- For WASI plugins in other languages: Rust (`wasm32-wasi` target), Zig,
  or any language with WASI Preview 1 support.

---

## Part 1: Plain-WASM Plugins (recommended default)

Plain-WASM plugins target `wasm32-unknown-unknown` — no WASI imports.
Every capability the plugin needs is a Lahijan host function, gated by
the permission enforcer. This is the simplest and most secure mode.

### Step 1: Scaffold

```sh
cp -r examples/plugins/_template my-plugin
cd my-plugin
```

The template includes:
- `main.go` — entry point with SDK imports (commented out)
- `go.mod` — references the SDK via a local `replace`
- `lahijan.manifest.yaml` — declares name, version, permissions
- `Makefile` — `build`, `verify`, `clean` targets

### Step 2: Declare permissions

Edit `lahijan.manifest.yaml`:

```yaml
name: my-autoscaler
version: 1.0.0
description: "Scales instances based on CPU metrics"

permissions:
  - "events.listen:compute.instance.*"
  - "compute.instance.create"
  - "compute.instance.control"
  - "kv.read:metrics"
  - "kv.write:metrics"

entrypoints:
  - on_event
```

Every permission must be in the [capability catalog](../architecture/plugins.md#the-permission-catalog).
The admin sees exactly this list at install time.

### Step 3: Write the plugin

```go
package main

import (
    "github.com/avestura/lahijan/sdk-go/compute"
    "github.com/avestura/lahijan/sdk-go/events"
    "github.com/avestura/lahijan/sdk-go/kv"
    "github.com/avestura/lahijan/sdk-go/mem"
)

//export on_event
func on_event(payloadPtr, payloadLen uint32) {
    payload := mem.Read(payloadPtr, payloadLen)
    _ = payload // parse event JSON, check CPU threshold...

    // Create a new instance to handle the load
    inst, err := compute.CreateInstance(compute.CreateInstanceParams{
        Name:       "worker-" + id,
        Type:       "container",
        ImageAlias: "ubuntu/24.04",
        Profiles:   []string{"default"},
    })
    if err != nil {
        return
    }

    // Record the scaling event in KV
    _ = kv.Set("last_scaled", []byte(inst.ID), 0)
}

//export on_init
func on_init() {
    // Subscribe to high-CPU events
    _ = events.Subscribe("compute.instance.cpu_high", "on_event")
}

func main() {}
```

No `unsafe.Pointer`. No `(ptr, len)` juggling. No status-code switches.
The SDK handles everything.

### Step 4: Build

```sh
make build      # tinygo build -target wasm -o plugin.wasm main.go
make verify     # assert no WASI imports leaked in
```

### Step 5: Install

```sh
# Upload
curl -F 'wasm=@plugin.wasm;type=application/wasm' \
     -F 'manifest=@lahijan.manifest.yaml;type=text/yaml' \
     -H 'Cookie: lahijan_session=...' \
     http://localhost:3000/api/v1/admin/plugins/upload

# Grant each declared permission
PLUGIN_ID=...
for slug in \
    'events.listen%3Acompute.instance.*' \
    'compute.instance.create' \
    'compute.instance.control' \
    'kv.read%3Ametrics' \
    'kv.write%3Ametrics'; do
  curl -X POST \
    "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/permissions/$slug/grant" \
    -H 'Cookie: lahijan_session=...'
done

# Enable
curl -X POST "http://localhost:3000/api/v1/admin/plugins/$PLUGIN_ID/enable" \
     -H 'Cookie: lahijan_session=...'
```

The plugin is now live. The next `compute.instance.cpu_high` event fires
`on_event`, which creates a new instance.

---

## Part 2: WASI Plugins

WASI plugins target `wasm32-wasi` and can use filesystem I/O, stdlib
assumptions, and existing third-party WASI modules. They run under the
**same permission model** as plain-WASM plugins — WASI capabilities
(filesystem preopens, environment variables) are gated by dedicated
permission slugs.

### When to use WASI

- You need **filesystem access** (e.g. a log-rotator that writes to disk)
- You are **reusing an existing WASI-compiled module** (e.g. a Rust crate)
- Your language's stdlib **assumes WASI** (e.g. Rust's `std::fs`)

### Step 1: Declare the WASI runtime in the manifest

```yaml
name: log-rotator
version: 1.0.0
description: "Writes structured logs to disk"

runtime: wasi

permissions:
  - "wasi.fs.preopen:/data:rw"
  - "wasi.env:LOG_FORMAT"
  - "wasi.clock"
  - "events.listen:compute.instance.*"
  - "kv.read:state"
  - "kv.write:state"

wasi:
  preopens:
    - guestPath: /data
      hostSubdir: data
      mode: rw
  env:
    - LOG_FORMAT
  clock: true
  random: true

entrypoints:
  - on_event
```

### Step 2: Build for the WASI target

**TinyGo:**
```sh
tinygo build -target wasi -o plugin.wasm main.go
```

**Rust:**
```sh
cargo build --target wasm32-wasi --release
cp target/wasm32-wasi/release/my_plugin.wasm plugin.wasm
```

### Step 3: Install + grant

Same install flow as plain-WASM. The admin sees the WASI permissions
alongside the Lahijan permissions and approves each individually.

### The `*` escape hatch

For fully-trusted plugins (e.g. internally-developed tools), the admin can
grant `*` instead of individual WASI slugs. This registers the full
`wasi_snapshot_preview1` surface with no filtering. The install UI shows
a loud warning.

```yaml
permissions:
  - "*"
```

`*` only affects WASI imports — it does NOT grant Lahijan host-function
permissions (KV, events, compute, etc.). Those still need explicit grants.

---

## Infrastructure Host Functions

Plugins can manage Lahijan infrastructure directly through dedicated host
functions. These delegate to the real services, which enforce tenant
scoping, billing, and audit — exactly as if the call came through the REST
API.

### Compute (instances)

```go
import "github.com/avestura/lahijan/sdk-go/compute"

// Create a container
inst, err := compute.CreateInstance(compute.CreateInstanceParams{
    Name:       "web-server",
    Type:       "container",      // or "virtual-machine"
    ImageAlias: "ubuntu/24.04",
    Profiles:   []string{"default"},
    Config:     map[string]string{"limits.cpu": "2", "limits.memory": "4GiB"},
})

// List instances
insts, err := compute.ListInstances(50, 0)

// Start / stop / restart
_, err = compute.SetInstanceState(inst.ID, "start", false, 0)
_, err = compute.SetInstanceState(inst.ID, "stop", false, 30)

// Delete
err = compute.DeleteInstance(inst.ID)
```

**Required permissions:** `compute.instance.create`, `.read`, `.control`,
`.delete`.

### DNS (zones + records)

```go
import "github.com/avestura/lahijan/sdk-go/dns"

// Create a zone
zone, err := dns.CreateZone(dns.CreateZoneParams{
    Name: "example.com.",
    Kind: "Native",
})

// Create a record
record, err := dns.CreateRecord(dns.CreateRecordParams{
    ZoneID:  zone.ID,
    Name:    "www.example.com.",
    Type:    "A",
    Content: "192.0.2.1",
    TTL:     300,
})

// List records in a zone
records, err := dns.ListRecords(zone.ID, 100, 0)
```

**Required permissions:** `dns.zone.create`, `.read`, `.delete`;
`dns.record.create`, `.read`, `.delete`.

### Storage (buckets)

```go
import "github.com/avestura/lahijan/sdk-go/storage"

// Create a bucket
bucket, err := storage.CreateBucket(storage.CreateBucketParams{
    Slug:  "my-data",
    Label: "Application Data",
})

// List buckets
buckets, err := storage.ListBuckets(50, 0)

// Delete
err = storage.DeleteBucket(bucket.ID)
```

**Required permissions:** `storage.bucket.create`, `.read`, `.delete`.

---

## Error Handling

Every SDK function returns a Go `error`. Status codes are translated to
typed, `errors.Is`-able sentinels:

```go
import "github.com/avestura/lahijan/sdk-go/status"
import "errors"

inst, err := compute.GetInstance(id)
switch {
case errors.Is(err, status.ErrNotFound):
    // instance doesn't exist
case errors.Is(err, status.ErrPermissionDenied):
    // compute.instance.read not granted
case errors.Is(err, status.ErrUnavailable):
    // compute service not wired in this process
case err != nil:
    // other host error
}
```

The full sentinel list: `ErrPermissionDenied`, `ErrNotFound`,
`ErrUnavailable`, `ErrBufferTooSmall`, `ErrUpstream`, `ErrInvalidArgument`,
`ErrInvalidMemory`, `ErrGenericFailure`.

---

## Testing

The SDK uses a build-tag pattern: under the standard Go toolchain
(`!tinygo`), the WASM imports are replaced by mockable function variables.
This means you can unit-test SDK-consuming code with standard `go test`:

```go
func TestAutoScaler(t *testing.T) {
    // Replace the host function with a mock
    compute.SetMockCreateInstance(func(args compute.CreateInstanceParams) (compute.Instance, error) {
        return compute.Instance{ID: "test-123", Name: args.Name}, nil
    })
    defer compute.ResetMockCreateInstance()

    // Call the function under test
    inst, err := compute.CreateInstance(...)
    require.NoError(t, err)
    assert.Equal(t, "test-123", inst.ID)
}
```

For end-to-end testing (real wazero runtime + Postgres), see the
integration tests in
`internal/app/lahijan/wasm/hostfuncs/hostfuncs_integration_test.go`.

---

## Non-Go Languages

For Rust, AssemblyScript, Zig, and other languages that target
`wasm32-unknown-unknown`, you cannot use the Go SDK. Instead, declare
the host-function imports directly using the raw ABI documented in
[Plugin Architecture → Host functions](../architecture/plugins.md#host-functions).

The `(ptr, len)` convention and status-code table are language-agnostic.
Each function follows the same shape: pass data as byte arrays at known
memory offsets, receive an i32 status code back.
