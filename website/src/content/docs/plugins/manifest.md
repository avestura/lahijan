---
title: Manifest reference
description: Every field of lahijan.manifest.yaml, the permission catalog, the matching rules and the validation Lahijan applies at upload.
---

Every plugin package contains a `lahijan.manifest.yaml` at its root. It tells Lahijan who the plugin is and which permissions it wants. Lahijan parses it on upload, when you run `lahx pack` or `lahx inspect`, and when it installs from a marketplace. A manifest that fails validation rejects the whole package, and the error lists every problem at once.

## Fields

| Field           | Type            | Required | Rules                                                                                                        |
| --------------- | --------------- | -------- | ------------------------------------------------------------------------------------------------------------ |
| `name`          | string          | yes      | 1 to 64 characters, lowercase letters `a-z`, digits and `-`. Must not start or end with `-` or contain `--`. |
| `version`       | string          | yes      | `MAJOR.MINOR.PATCH`, each part numeric. A `-pre` or `+build` suffix on the patch part is accepted.           |
| `description`   | string          | no       | Shown in the dashboard.                                                                                      |
| `author`        | string          | no       | Free text, for example `Name <email>`.                                                                       |
| `license`       | string          | no       | An SPDX identifier such as `Apache-2.0`. Not validated.                                                      |
| `homepage`      | string          | no       | Not validated.                                                                                               |
| `permissions`   | list of strings | no       | Each entry must be a valid permission (see below). An empty or missing list is allowed.                      |
| `config_schema` | object          | no       | A JSON-schema style description of config keys. Stored, not validated.                                       |
| `entrypoints`   | list of strings | no       | Names of exports the host may call. Entries must not be empty or blank.                                      |
| `runtime`       | string          | no       | Empty, `none` or `wasi`. Anything else is rejected.                                                          |
| `wasi`          | object          | no       | Only read when `runtime: wasi`. See [WASI block](#wasi-block).                                               |

Unknown fields are ignored. Memory size is not a manifest field: the runtime reads it from the module and checks it against `wasm.maxMemoryPerPlugin`.

The name and version pair must be unique. Uploading a plugin whose name and version already exist returns `409 conflict`.

### config_schema

`config_schema` follows a JSON-schema shape: `type: object` with `properties`, where each property may have `type`, `format`, `default`, `description` and `secret: true`. Keys marked secret are never returned to the plugin by `config_get`.

> [!NOTE]
> Lahijan stores `config_schema` but has no endpoint or dashboard screen to set config values yet. See [current limitations](/docs/plugins/overview#current-limitations).

### entrypoints

List the exports your plugin expects the host to call, such as `on_event`. The runtime does not enforce this list in this release; it is recorded with the plugin and shown to the admin.

### WASI block

| Field           | Type            | Meaning                                                                                                                                                                                                |
| --------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `wasi.preopens` | list            | Directories to mount. Each entry has `guestPath` (must start with `/`, must not contain `..`), `hostSubdir` (required, must not contain `..`) and `mode` (`ro`, `rw` or empty, which means read-only). |
| `wasi.env`      | list of strings | Environment variable names the plugin wants.                                                                                                                                                           |
| `wasi.clock`    | bool            | Clock access.                                                                                                                                                                                          |
| `wasi.random`   | bool            | Random number access.                                                                                                                                                                                  |

> [!WARNING]
> The server does not register the WASI imports in this release, so a module that imports them fails to instantiate even though the manifest validates. Build plain modules with `runtime` left empty.

## Permissions

A permission has the form `scope.action` with an optional `:qualifier`. Validation rejects an entry when:

- it is empty, or the part before `:` has no `.`;
- the `scope.action` part is not in the catalog below;
- it has a `:` followed by nothing or only whitespace.

The single `*` is also accepted; it is the WASI wildcard described at the end of the table.

### Catalog

| Permission                 | Qualifier                                             | Gates                                                                                 |
| -------------------------- | ----------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `network.outbound`         | none (see note)                                       | `lahijan_network.http_request`                                                        |
| `kv.read`                  | none (see note)                                       | `lahijan_kv.get`                                                                      |
| `kv.write`                 | none (see note)                                       | `lahijan_kv.set`, `lahijan_kv.delete`                                                 |
| `config.read`              | none (see note)                                       | `lahijan_config.get`                                                                  |
| `events.emit`              | none                                                  | `lahijan_events.emit`                                                                 |
| `events.listen`            | topic pattern, for example `dns.record.*`             | `lahijan_events.subscribe`                                                            |
| `job.schedule`             | none                                                  | `lahijan_jobs.schedule`                                                               |
| `api.handler.register`     | sub-path, for example `/webhook`, or `*`              | `lahijan_api.register_handler`                                                        |
| `compute.instance.create`  | none                                                  | `instance_create`                                                                     |
| `compute.instance.read`    | none                                                  | `instance_get`, `instance_list`                                                       |
| `compute.instance.update`  | none                                                  | No host function uses it yet.                                                         |
| `compute.instance.control` | none                                                  | `instance_set_state`                                                                  |
| `compute.instance.delete`  | none                                                  | `instance_delete`                                                                     |
| `dns.zone.create`          | none                                                  | `zone_create`                                                                         |
| `dns.zone.read`            | none                                                  | `zone_get`, `zone_list`                                                               |
| `dns.zone.delete`          | none                                                  | `zone_delete`                                                                         |
| `dns.record.create`        | none                                                  | `record_create`                                                                       |
| `dns.record.read`          | none                                                  | `record_list`                                                                         |
| `dns.record.delete`        | none                                                  | `record_delete`                                                                       |
| `storage.bucket.create`    | none                                                  | `bucket_create`                                                                       |
| `storage.bucket.read`      | none                                                  | `bucket_get`, `bucket_list`                                                           |
| `storage.bucket.delete`    | none                                                  | `bucket_delete`                                                                       |
| `wasi.fs.preopen`          | guest path with optional mode, for example `/data:rw` | WASI filesystem mounts                                                                |
| `wasi.env`                 | variable name                                         | WASI environment variables                                                            |
| `wasi.clock`               | none                                                  | WASI clock                                                                            |
| `wasi.random`              | none                                                  | WASI random                                                                           |
| `wasi.exit`                | none                                                  | WASI `proc_exit`                                                                      |
| `*`                        | none                                                  | The full WASI surface without filtering. It does not grant any Lahijan host function. |

> [!WARNING]
> The validator accepts a qualifier on any permission, but the `network.outbound`, `kv.*` and `config.read` host functions check the bare name. A grant of `network.outbound:hooks.slack.com` or `kv.read:cache` does not allow the call. Request those four permissions without a qualifier. The example manifests in `examples/plugins/` use qualified forms and are affected by this.

### Matching rules

When a host function checks a permission, a grant matches when:

- it is exactly the requested string; or
- it ends in `:*` and the request has the same `scope.action:` with any non-empty qualifier (`events.listen:*` matches `events.listen:dns.record.created`); or
- it ends in `.*` after a qualifier and the request starts with the grant's prefix (`events.listen:dns.record.*` matches `events.listen:dns.record.created`, not `events.listen:dns.zone.created`).

Scope-level wildcards such as `kv.*` and mid-word wildcards such as `kv.read:rec*` never match.

For `events.listen`, the host checks `events.listen:<pattern>` using the exact pattern the plugin passes to `subscribe`. For `api.handler.register`, it checks `api.handler.register:<path>` with the exact path.

## Full example

```yaml title="lahijan.manifest.yaml"
name: instance-notifier
version: 1.2.0
description: "Posts instance lifecycle events to a webhook."
author: "Example Corp <ops@example.com>"
license: Apache-2.0
homepage: https://example.com/instance-notifier

permissions:
  - "events.listen:compute.instance.*"
  - "network.outbound"
  - "config.read"
  - "kv.read"
  - "kv.write"
  - "job.schedule"

config_schema:
  type: object
  properties:
    webhook_url:
      type: string
      format: uri
      secret: true
      description: "Where to post events."
    channel:
      type: string
      default: "#alerts"

entrypoints:
  - on_event
```

## Validation errors

When several fields are wrong, the error joins every message, for example:

```text
manifest: multiple errors:
  - manifest: name: contains invalid character 'M' at index 0 (allowed: a-z 0-9 -)
  - manifest: permissions[1] "kv.*": permission: "kv.*" is not in the capability catalog: permission: unknown slug
```

The upload API returns this text in the `message` of a `400 bad_request` response.
