# autoscaler-stub

A sample Lahijan plugin that listens for `compute.instance.cpu_high`
events and emits a comment into the audit log via the event bus. This is
the skeleton a future real autoscaler would build on — it demonstrates
the events.listen + kv.read/write patterns without coupling to any
specific scaling backend.

## Permissions declared

| Slug | Why |
|------|-----|
| `events.listen:compute.instance.cpu_high` | Get notified when CPU crosses the threshold. |
| `kv.read:metrics` | Read the running counter of recent alerts. |
| `kv.write:metrics` | Increment the counter when an alert fires. |
| `events.emit` | Emit a `plugin.autoscaler.tick` event the audit log picks up. |

## Build

```sh
make build
```

## What it does

1. On every `compute.instance.cpu_high` event, increment the
   `cpu_high_count` key in the `metrics` KV namespace.
2. If the counter crosses a configurable threshold (default 3 within the
   TTL window), emit a `plugin.autoscaler.tick` event the audit log
   records and a real downstream consumer (an autoscaler worker) could
   pick up.
3. Reset the counter on each emit.

The plugin is intentionally a stub: no actual scaling happens. A real
autoscaler would replace the `events.emit` call with a `compute.instance.*`
host function that scales the instance; Lahijan's host-function surface
grows as Phase 4 modules ship.
