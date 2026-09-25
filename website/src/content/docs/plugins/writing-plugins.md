---
title: Writing a plugin
description: Build a Lahijan plugin from the template with TinyGo and the Go SDK, package it as a .lahx file and install it.
---

This page walks through building a plugin from the template in the Lahijan repository, `examples/plugins/_template/`. You end with a `.lahx` package you can upload in the dashboard.

Read [How plugins work](/docs/plugins/overview) first, in particular the [current limitations](/docs/plugins/overview#current-limitations): they affect what a plugin can do today.

## Toolchain

The template and the example plugins use:

| Tool                                                                | Why                                                                         |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| [TinyGo](https://tinygo.org/getting-started/install/) 0.32 or newer | Compiles Go to a plain WebAssembly module with `tinygo build -target wasm`. |
| Go 1.23 or newer                                                    | Runs `cmd/lahx`, the packaging tool, and lets editors type-check the SDK.   |
| GNU Make                                                            | Drives the build, verify and package targets.                               |

The standard `go build` is not used for the module itself. Other languages work too, as long as the output is a `wasm32-unknown-unknown` module that imports the host functions with the signatures on the [Host API](/docs/plugins/host-api) page. The SDK and examples only cover Go.

## Project layout

Copy the template next to the other examples so its relative paths keep working:

```sh
cp -r examples/plugins/_template examples/plugins/my-plugin
cd examples/plugins/my-plugin
```

You get:

```text
my-plugin/
  README.md
  lahijan.manifest.yaml   # name, version, permissions
  main.go                 # plugin code
  go.mod                  # module definition, depends on the SDK
  Makefile                # build, verify, package, clean
```

The `go.mod` pulls in the SDK from the repository checkout:

```text title="go.mod"
module example.com/your-plugin

go 1.23

require github.com/avestura/lahijan/sdk-go v0.1.0

replace github.com/avestura/lahijan/sdk-go => ../../../sdk-go
```

If you move the plugin elsewhere, fix the `replace` path.

## Edit the manifest

Set a kebab-case name, a semver version and the permissions you need. Keep the list short: the admin approves each entry by hand.

```yaml title="lahijan.manifest.yaml"
name: my-plugin
version: 0.1.0
description: "Counts stopped instances."
author: "Your Name <you@example.com>"
license: Apache-2.0

permissions:
  - "events.listen:compute.instance.stopped"
  - "kv.read"
  - "kv.write"

entrypoints:
  - on_event
```

Use the bare `kv.read` and `kv.write` forms: the key-value host functions check the unqualified permission, so `kv.read:state` would be denied at run time. See the [Manifest reference](/docs/plugins/manifest) for every field.

## Write the code

The SDK hides pointer handling and status codes. Each host module has a package: `kv`, `config`, `events`, `jobs`, `network`, `api`, `compute`, `dns`, `storage`, plus `mem` for memory helpers and `status` for errors.

```go title="main.go"
package main

import (
	"errors"
	"strconv"

	"github.com/avestura/lahijan/sdk-go/kv"
	"github.com/avestura/lahijan/sdk-go/status"
)

//export on_event
func on_event() {
	n := 0
	if raw, err := kv.Get("stopped_count"); err == nil {
		n, _ = strconv.Atoi(string(raw))
	} else if !errors.Is(err, status.ErrNotFound) {
		return // denied, unavailable, ...
	}
	_ = kv.Set("stopped_count", []byte(strconv.Itoa(n+1)), 0)
}

// main is required by TinyGo. The host never calls it.
func main() {}
```

Rules to follow:

- Mark every function the host may call with `//export <name>`.
- Keep a `main` function, even if it is empty.
- Check errors. SDK calls return `status.ErrPermissionDenied` when a grant is missing, `status.ErrUnavailable` when the operator has turned the backing feature off, and `status.ErrNotFound` for missing keys.
- Keep each call short. A call is stopped after `wasm.execTimeoutMs` (5 seconds by default). Split long work into scheduled jobs with `jobs.Schedule`.

> [!NOTE]
> The template declares `on_event(payloadPtr, payloadLen uint32)`. In this release the host calls handler exports with no arguments, so the event payload is not delivered and a two-parameter export fails to run. A zero-parameter export, as above, runs. See [current limitations](/docs/plugins/overview#current-limitations).

## Build

```sh
make build
```

This runs `tinygo build -target wasm -o plugin.wasm main.go`. Then check that the module has no WASI imports:

```sh
make verify
```

`verify` fails if the module imports `wasi_snapshot_preview1` or `wasi_unstable`.

## Package

```sh
make package
```

This runs `go run ./cmd/lahx pack` from the repository root. The tool validates the manifest with the same rules the server uses, checks that `plugin.wasm` starts with the WebAssembly magic bytes, and writes `my-plugin-0.1.0.lahx` into the plugin directory.

Outside the repository, set `LAHIJAN_ROOT` to a Lahijan checkout, or build the archive by hand:

```sh
zip -j my-plugin-0.1.0.lahx lahijan.manifest.yaml plugin.wasm
```

Inspect the result before uploading:

```console
$ go run ./cmd/lahx inspect examples/plugins/my-plugin/my-plugin-0.1.0.lahx
my-plugin 0.1.0
  module: 18342 bytes
  permissions: [events.listen:compute.instance.stopped kv.read kv.write]
```

See [Packaging (.lahx)](/docs/plugins/packaging) for the format.

## Test locally

There is no local plugin runner. The practical checks before upload are:

- `make verify` for WASI imports.
- `lahx pack` and `lahx inspect` for manifest errors.
- Unit tests for your logic under the standard Go toolchain. When the SDK is built with `go` instead of TinyGo, its host imports are replaced by function variables, so your code type-checks and your pure logic can be tested with `go test`.

For an end-to-end check, install the plugin on a development Lahijan with `wasm.enabled` and `jobs.enabled` set to `true`, and watch the `wasm.host.*` spans and the `plugins` job queue.

## Install

Upload the `.lahx` file under **Administration > Plugins > Upload plugin**, grant the permissions, then enable it. The steps are in [Installing plugins](/docs/admin/plugins). From a script:

```sh
curl -X POST https://lahijan.example.com/api/v1/admin/plugins/upload \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -F "package=@my-plugin-0.1.0.lahx"
```

## Release a new version

Bump `version` in the manifest, rebuild and repackage. Uploading the same name and version twice is refused with `409 conflict`. To replace an installed version in place and keep its grants, publish it through a [marketplace](/docs/admin/marketplace) index and use **Upgrade**.
