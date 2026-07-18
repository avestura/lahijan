# Sample plugins shipped with Lahijan (WS-10c).

Each subdirectory is a self-contained Lahijan plugin. Build it with
`make build` (requires [TinyGo](https://tinygo.org) on PATH) and upload
the resulting `plugin.wasm` via the admin API.

| Plugin | Listens to | Calls | Used to demo |
|--------|------------|-------|--------------|
| `slack-notifier` | `compute.instance.*` | network.outbound + config.read | posting alerts to Slack |
| `autoscaler-stub` | `compute.instance.cpu_high` | kv.* + events.emit | event-driven scaling skeleton |
| `dns-record-hook` | `dns.record.*` | network.outbound + config.read | webhook forwarding |
| `_template/` | (none) | (none) | starting point for new plugins |

## Marketplace

The `marketplace/` subdirectory is the in-repo default marketplace index.
Operators may replace it or point `wasm.marketplace.url` at a remote URL
serving the same shape. See `marketplace/README.md`.

## Build all

There is no top-level Makefile because TinyGo's module resolution is
per-directory. Build each plugin individually:

```sh
for p in slack-notifier autoscaler-stub dns-record-hook; do
  (cd $p && make build)
done
```

## Run tests

The Go-side marketplace + installer tests live under
`internal/app/lahijan/wasm/marketplace/`. The plugin sources themselves
are not Go-test-covered (TinyGo is a separate toolchain); they are
exercised end-to-end via the WS-22 sandbox harness (future WS).

## More

See `docs/architecture/plugins.md` in the Lahijan repo for the full
developer guide (manifest schema, host-function reference, build
process, install flow, permission reference, "write your first plugin"
tutorial).
