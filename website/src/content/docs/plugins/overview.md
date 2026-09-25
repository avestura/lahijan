---
title: How plugins work
description: What a Lahijan plugin is, how the permission model works, the plugin lifecycle and the limits the sandbox enforces.
---

A Lahijan plugin is a WebAssembly module that runs inside the Lahijan process, in a sandbox built on [wazero](https://wazero.io). A plugin cannot open sockets, read files or see environment variables. Everything it can do goes through a small set of host functions that Lahijan exposes, and every host function checks a permission before it acts.

This page explains the model. To build one, see [Writing a plugin](/docs/plugins/writing-plugins). To install one, see [Installing plugins](/docs/admin/plugins).

## What a plugin is made of

A plugin ships as a single `.lahx` file, which is a ZIP archive with two files at its root:

| File                    | Purpose                                                                                                             |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `lahijan.manifest.yaml` | Declares the plugin's name, version and the permissions it wants. See [Manifest reference](/docs/plugins/manifest). |
| `plugin.wasm`           | The compiled WebAssembly module.                                                                                    |

See [Packaging (.lahx)](/docs/plugins/packaging) for the format and its limits.

The module is a plain `wasm32-unknown-unknown` build with no WASI imports. The examples in the repository are written in Go and compiled with TinyGo, using the plugin SDK in `sdk-go/`. Any language that can produce a WebAssembly module and call imported functions with the documented signatures can target the same [Host API](/docs/plugins/host-api).

## The permission model

Plugins use an install-time permission model, similar to mobile apps:

1. The manifest lists every permission the plugin wants, for example `events.emit` or `network.outbound`.
2. When an admin uploads the plugin, Lahijan validates each entry against a fixed catalog. An unknown permission rejects the upload.
3. The plugin is stored in the `pending` state with no permissions granted.
4. An admin reviews the requested list and grants each permission one by one. Nothing is granted automatically.
5. On every host function call, Lahijan checks the plugin's grants. A missing grant fails closed: the call returns the status code `-2` (denied) and the plugin keeps running.

Revoking a permission takes effect on the next host call. There is no restart.

Permission names follow the `scope.action` form, with an optional `:qualifier` suffix (for example `events.listen:dns.record.*`). The full catalog and the matching rules are in the [Manifest reference](/docs/plugins/manifest#permissions).

> [!WARNING]
> Only some host functions look at the qualifier. The key-value, config and outbound HTTP functions check the bare permission (`kv.read`, `kv.write`, `config.read`, `network.outbound`). A grant such as `kv.read:cache` does not satisfy them. Request and grant the bare form for those capabilities. The details are on the [Host API](/docs/plugins/host-api#permissions-checked) page.

## Lifecycle

A plugin row has one of three states:

| State      | Meaning                                                                                      |
| ---------- | -------------------------------------------------------------------------------------------- |
| `pending`  | Uploaded and validated, not yet enabled. Every upload and every upgrade lands here.          |
| `active`   | Enabled by an admin.                                                                         |
| `disabled` | Turned off by an admin. Grants are kept, so enabling it again restores the same permissions. |

The transitions are:

- **Upload** (or install from the [marketplace](/docs/admin/marketplace)) creates the row in `pending`. Lahijan compiles the module first and rejects it if it does not compile or declares more memory than the configured cap.
- **Enable** moves it to `active`. **Disable** moves it to `disabled`. Enabling does not require any grants; a plugin with no grants loads, but every host call is denied.
- **Upgrade** installs a newer version from the marketplace. The new version lands in `pending`, grants the new manifest still requests are carried over, grants it no longer requests are dropped, and new permissions wait for approval. The old row is deleted. Downgrades and same-version upgrades are refused.
- **Delete** (uninstall) removes the row and, by cascade, its grants, key-value data, config, event subscriptions and registered HTTP routes.

Every one of these actions writes an entry to the [audit log](/docs/audit/overview) (`plugins.upload`, `plugins.grant`, `plugins.revoke`, `plugins.enable`, `plugins.disable`, `plugins.delete`, `plugins.marketplace_install`, `plugins.marketplace_upgrade`, and `plugins.upgrade`).

## What a plugin can do

The host functions are grouped into modules:

| Module            | What it does                                           | Permission                             |
| ----------------- | ------------------------------------------------------ | -------------------------------------- |
| `lahijan_kv`      | Per-plugin key-value store                             | `kv.read`, `kv.write`                  |
| `lahijan_config`  | Read admin-set, non-secret config values               | `config.read`                          |
| `lahijan_events`  | Emit events, subscribe to event topics                 | `events.emit`, `events.listen:<topic>` |
| `lahijan_jobs`    | Schedule a call to one of the plugin's own exports     | `job.schedule`                         |
| `lahijan_api`     | Register an HTTP route under `/api/v1/plugins/<name>/` | `api.handler.register:<path>`          |
| `lahijan_network` | Outbound HTTP requests                                 | `network.outbound`                     |
| `lahijan_compute` | Create, read, control and delete instances             | `compute.instance.*`                   |
| `lahijan_dns`     | Manage zones and records                               | `dns.zone.*`, `dns.record.*`           |
| `lahijan_storage` | Manage buckets                                         | `storage.bucket.*`                     |

The compute, DNS and storage functions act inside the tenant the plugin was installed into. They go through the same services as the dashboard, so tenant scoping, quotas, billing and audit apply.

## How plugin code runs

Lahijan calls into a plugin through a background job of kind `wasm.plugin.invoke` on the `plugins` queue (see [Background jobs](/docs/admin/jobs)). Each job names the plugin and one exported function. The worker creates a fresh instance of the module, calls the export, and discards the instance. Nothing survives between calls except what the plugin stored through the host (key-value data, subscriptions, routes).

Two things create these jobs:

- **Events.** When an event is emitted, Lahijan looks up every subscription whose topic pattern matches and queues one job per match, naming the handler export the plugin gave when it subscribed.
- **Scheduled jobs.** A plugin can queue a future call to one of its own exports with `lahijan_jobs.schedule`.

Jobs need the job queue (`jobs.enabled: true`) as well as the plugin runtime.

## Current limitations

These gaps are real in this release. Plan around them.

- **No start-up hook.** Lahijan does not call any export when a plugin is enabled. Subscriptions, routes and scheduled jobs are only created by plugin code, and plugin code only runs from the jobs described above.
- **Event payloads are not passed in.** The worker calls the handler export with no arguments. An export declared as `on_event(ptr, len uint32)`, as in the template, does not receive the event body.
- **Plugin HTTP routes are recorded but not served.** `register_handler` stores the route, but Lahijan does not yet route requests under `/api/v1/plugins/` to plugins.
- **No config endpoint.** The manifest's `config_schema` is stored, but there is no API or dashboard screen to set config values, so `config_get` returns "not found" unless values were written to the database directly.
- **`entrypoints` is informational.** The runtime does not restrict calls to the listed exports.
- **Status is not checked at run time.** The job worker does not check whether the plugin is `active`. Disable a plugin and revoke its grants if you need to stop it.
- **Subscriptions are not tenant-filtered.** A subscription matches on the topic alone, so a plugin receives matching events from every tenant.
- **WASI mode is declared but not wired.** The manifest accepts `runtime: wasi`, but the server does not register the WASI imports, so a module that imports them fails to instantiate.

## Resource limits

The operator sets process-wide caps in the `wasm` block of the config file. See [Configuration](/docs/reference/configuration) for the full reference.

| Key                       | Default             | What it limits                                                                                                                            |
| ------------------------- | ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `wasm.enabled`            | `false`             | Turns the plugin runtime on. When off, the admin plugin API returns `501 not_implemented`. The production compose file sets it to `true`. |
| `wasm.maxMemoryPerPlugin` | `33554432` (32 MiB) | Linear memory per instance. A module whose exported memory minimum or maximum is above the cap is rejected at upload.                     |
| `wasm.execTimeoutMs`      | `5000`              | Wall-clock time for one call into the plugin. A plugin that loops is stopped when this expires.                                           |
| `wasm.maxModuleSize`      | `10485760` (10 MiB) | Size of `plugin.wasm` inside an uploaded package.                                                                                         |

Host functions add their own limits: 1 KiB keys and 256 KiB values in the key-value store, 64 KiB event payloads and job arguments, 1 MiB HTTP bodies with a 10 second timeout, and jobs scheduled at most 30 days ahead. The [Host API](/docs/plugins/host-api) page lists them per function.

## Observability

Every host call opens an OpenTelemetry span named `wasm.host.<module>.<function>` with the plugin id, the permission slug and the result code (`wasm.host.result`, for example `denied` or `buffer_too_small`). Failed calls are also logged at debug level. See [Observability](/docs/operations/observability) for where spans and logs go.
