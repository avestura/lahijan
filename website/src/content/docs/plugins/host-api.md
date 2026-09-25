---
title: Host API
description: Every host function Lahijan exposes to plugins, with its signature, required permission, limits, status codes and the event topics plugins can subscribe to.
---

Plugins talk to Lahijan by calling imported host functions. This page is the reference for the raw ABI: module and function names, parameters, return values and the permission each call checks. If you write in Go, the SDK in `sdk-go/` wraps all of this; the SDK function is named next to each host function.

## Calling convention

- All parameters are `i32` except where noted as `i64`. Every function returns one `i32`.
- Strings and byte blobs are passed as a pointer and a length into the plugin's own linear memory.
- Functions that return data write it into a buffer the plugin provides (`buf_ptr`, `buf_cap`). On success they return the number of bytes written. If the buffer is too small they return `-7` and write nothing; allocate a bigger buffer and call again. The SDK does this for you.
- Every call first resolves the calling plugin, then checks its permission, then acts. A denied call returns `-2`; it does not trap.

### Status codes

| Code  | Name                | Meaning                                                                                                                    |
| ----- | ------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `0`   | success             | Done. For read functions, the value exists and is empty.                                                                   |
| `> 0` | success with length | Bytes written into your buffer (or the HTTP status for `http_request`).                                                    |
| `-1`  | generic failure     | Unexpected error. The host logs the cause; the plugin only sees the code.                                                  |
| `-2`  | denied              | The plugin lacks the permission.                                                                                           |
| `-3`  | unavailable         | The operator turned off the backing feature (for example no database, no job queue, compute not configured). Do not retry. |
| `-4`  | invalid memory      | A pointer and length fall outside your memory. A bug in the plugin.                                                        |
| `-5`  | invalid argument    | Empty or oversized input, bad path, bad JSON.                                                                              |
| `-6`  | not found           | Missing key or row.                                                                                                        |
| `-7`  | buffer too small    | Grow your buffer and retry.                                                                                                |
| `-8`  | upstream error      | An outbound HTTP request failed to complete.                                                                               |

In Go, `status.FromCode` turns these into `status.ErrPermissionDenied`, `status.ErrNotFound` and so on.

### Permissions checked

| Function                              | Permission checked            |
| ------------------------------------- | ----------------------------- |
| `lahijan_kv.get`                      | `kv.read` (bare)              |
| `lahijan_kv.set`, `lahijan_kv.delete` | `kv.write` (bare)             |
| `lahijan_config.get`                  | `config.read` (bare)          |
| `lahijan_network.http_request`        | `network.outbound` (bare)     |
| `lahijan_events.emit`                 | `events.emit`                 |
| `lahijan_events.subscribe`            | `events.listen:<pattern>`     |
| `lahijan_events.unsubscribe`          | none                          |
| `lahijan_jobs.schedule`               | `job.schedule`                |
| `lahijan_api.register_handler`        | `api.handler.register:<path>` |
| `lahijan_api.unregister_handler`      | none                          |
| compute, DNS, storage functions       | see their sections            |

"Bare" means the host asks for the permission with no qualifier, so only a grant of exactly that name matches. See [matching rules](/docs/plugins/manifest#matching-rules).

## Key-value store: lahijan_kv

Each plugin has its own key-value namespace. The host adds the plugin id to every query, so one plugin cannot read another's keys. Keys are at most 1 KiB, values at most 256 KiB.

| Function | Signature                                           | Returns                                                                                         |
| -------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `get`    | `(key_ptr, key_len, buf_ptr, buf_cap)`              | Bytes written, `0` for an empty value, `-6` missing, `-7` buffer too small                      |
| `set`    | `(key_ptr, key_len, val_ptr, val_len, ttl_ms: i64)` | `0`. `ttl_ms` of `0` means no expiry; otherwise the value expires after that many milliseconds. |
| `delete` | `(key_ptr, key_len)`                                | `0`, also when the key did not exist                                                            |

SDK: `kv.Get`, `kv.Set`, `kv.Delete`.

## Config: lahijan_config

| Function | Signature                              | Returns                                                                             |
| -------- | -------------------------------------- | ----------------------------------------------------------------------------------- |
| `get`    | `(key_ptr, key_len, buf_ptr, buf_cap)` | Bytes of the JSON-encoded value, `-6` when missing or secret, `-7` buffer too small |

Keys are at most 256 bytes. Secret values are never returned, and the plugin cannot tell a secret key from a missing one. SDK: `config.Get`, `config.GetString`.

## Events: lahijan_events

| Function      | Signature                                          | Returns                                                                                     |
| ------------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `emit`        | `(topic_ptr, topic_len, payload_ptr, payload_len)` | `0`. `-5` for an empty topic or one over 256 bytes; `-1` if the payload is over 64 KiB.     |
| `subscribe`   | `(topic_ptr, topic_len, handler_ptr, handler_len)` | `0`. Idempotent. `-5` for an empty or oversized topic or handler name (max 256 bytes each). |
| `unsubscribe` | `(topic_ptr, topic_len, handler_ptr, handler_len)` | `0`                                                                                         |

`subscribe` stores a durable row: topic pattern plus the name of the export to call. When a matching event is emitted, Lahijan queues a `wasm.plugin.invoke` job that calls that export. See [How plugin code runs](/docs/plugins/overview#how-plugin-code-runs) and its limitations: the export is called with no arguments.

Topic patterns match like this: an exact topic matches itself; a pattern ending in `.*` matches one more segment only (`dns.record.*` matches `dns.record.created`, not `dns.record.a.b`); a bare `*` matches every topic.

A plugin may emit any topic, including its own names such as `my-plugin.tick.done`.

SDK: `events.Emit`, `events.Subscribe`, `events.Unsubscribe`.

## Jobs: lahijan_jobs

| Function   | Signature                                                       | Returns                                                                                                                                         |
| ---------- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `schedule` | `(name_ptr, name_len, args_ptr, args_len, run_at_unix_ms: i64)` | `0`. `-5` if the name is empty or over 256 bytes, args exceed 64 KiB, or the run time is more than 30 days ahead. `-3` if the job queue is off. |

`name` is one of your own exports. A run time in the past runs as soon as possible. The job is durable across restarts and retried by the job queue on failure. If the export no longer exists when the job runs (for example after an upgrade), the job completes without calling anything. The `args` bytes are stored with the job but are not passed to the export in this release.

SDK: `jobs.Schedule`.

## HTTP routes: lahijan_api

| Function             | Signature                                                                | Returns                                                                                                                                                                            |
| -------------------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `register_handler`   | `(method_ptr, method_len, path_ptr, path_len, handler_ptr, handler_len)` | `0`. Idempotent. `-5` for an empty method or one over 16 bytes, a path that is empty, over 1024 bytes, does not start with `/` or contains `..` or `//`, or an empty handler name. |
| `unregister_handler` | `(method_ptr, method_len, path_ptr, path_len)`                           | `0`                                                                                                                                                                                |

The method is upper-cased. Routes are meant to mount under `/api/v1/plugins/<plugin-name>/<path>`.

> [!NOTE]
> The route is recorded, but Lahijan does not yet serve requests to plugin routes.

SDK: `api.RegisterHandler`, `api.UnregisterHandler`.

## Outbound HTTP: lahijan_network

```text
http_request(method_ptr, method_len, url_ptr, url_len,
             req_headers_ptr, req_headers_len, req_body_ptr, req_body_len,
             resp_hdr_buf_ptr, resp_hdr_buf_cap,
             resp_body_buf_ptr, resp_body_buf_cap) -> i32
```

- Request headers are a JSON object of header name to value.
- On success the return value is the HTTP status code (100 to 599).
- If both response buffer capacities are `0`, only the status is returned.
- Otherwise each buffer receives a 4-byte little-endian length followed by the data: the response headers as a JSON object (first value of each header) and the body. If either buffer is too small the call returns `-7` and writes nothing.

Limits: URL 8 KiB, request headers 8 KiB, request and response bodies 1 MiB (longer responses are cut at 1 MiB), 10 second timeout. A request that fails to complete returns `-8`. There is no per-plugin URL allowlist; any URL the server can reach is allowed.

SDK: `network.Do`, `network.Get`, `network.Post`, `network.PostJSON`.

## Compute, DNS and storage

These functions all use the same shape: `(args_ptr, args_len, buf_ptr, buf_cap)`. The arguments are a JSON object; the result is JSON written into your buffer. They return the byte count, `-6` when the row does not exist, `-7` when the buffer is too small, or `-1` for any other error (including invalid JSON or ids).

They run in the tenant the plugin was installed into, through the same services as the dashboard, so quotas, billing and audit apply. A plugin with no tenant cannot use them.

### lahijan_compute

| Function             | Permission                 | Arguments                                                                                  |
| -------------------- | -------------------------- | ------------------------------------------------------------------------------------------ |
| `instance_create`    | `compute.instance.create`  | `name`, `type`, `image_alias`, `profiles` (list), `config` (map of strings)                |
| `instance_get`       | `compute.instance.read`    | `id`                                                                                       |
| `instance_list`      | `compute.instance.read`    | `limit` (default 50), `offset`                                                             |
| `instance_set_state` | `compute.instance.control` | `id`, `action` (`start`, `stop`, `restart`, `freeze`, `unfreeze`), `force`, `timeout_secs` |
| `instance_delete`    | `compute.instance.delete`  | `id`                                                                                       |

Results are instance objects with fields such as `id`, `name`, `type`, `status`, `image_alias` and `profiles`. `instance_delete` returns `{"deleted": true, "id": "..."}`. SDK: the `compute` package.

### lahijan_dns

| Function        | Permission          | Arguments                                                                                  |
| --------------- | ------------------- | ------------------------------------------------------------------------------------------ |
| `zone_create`   | `dns.zone.create`   | `name` (for example `example.com.`), `description`, `kind` (`Native`, `Master` or `Slave`) |
| `zone_get`      | `dns.zone.read`     | `id`                                                                                       |
| `zone_list`     | `dns.zone.read`     | `limit`, `offset`                                                                          |
| `zone_delete`   | `dns.zone.delete`   | `id`                                                                                       |
| `record_create` | `dns.record.create` | `zone_id`, `name`, `type`, `content`, `ttl`                                                |
| `record_list`   | `dns.record.read`   | `zone_id`, `limit`, `offset`                                                               |
| `record_delete` | `dns.record.delete` | `zone_id`, `record_id`                                                                     |

SDK: the `dns` package.

### lahijan_storage

| Function        | Permission              | Arguments                                                      |
| --------------- | ----------------------- | -------------------------------------------------------------- |
| `bucket_create` | `storage.bucket.create` | `slug`, `label`, `description`, `quota_bytes`, `quota_objects` |
| `bucket_get`    | `storage.bucket.read`   | `id`                                                           |
| `bucket_list`   | `storage.bucket.read`   | `limit`, `offset`                                              |
| `bucket_delete` | `storage.bucket.delete` | `id`                                                           |

SDK: the `storage` package.

## Event topics

These topics are emitted by Lahijan in this release. Subscribe with `events.listen:<pattern>` granted.

| Area                  | Topics                                                                                                                                                                                                                                                                                                                   |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Compute instances     | `compute.instance.created`, `compute.instance.started`, `compute.instance.stopped`, `compute.instance.restarted`, `compute.instance.deleted`                                                                                                                                                                             |
| Snapshots and backups | `compute.snapshot.taken`, `compute.backup.created`, `compute.backup.deleted`                                                                                                                                                                                                                                             |
| Floating IPs          | `compute.ip.assigned`, `compute.ip.released`                                                                                                                                                                                                                                                                             |
| DNS zones             | `dns.zone.created`, `dns.zone.updated`, `dns.zone.deleted`, `dns.zone.dnssec.enabled`, `dns.zone.dnssec.disabled`, `dns.zone.dnssec.rotated`                                                                                                                                                                             |
| DNS records           | `dns.record.created`, `dns.record.updated`, `dns.record.deleted`                                                                                                                                                                                                                                                         |
| Domains               | `dns.domain.registered`, `dns.domain.renewed`, `dns.domain.transferred`, `dns.domain.deleted`                                                                                                                                                                                                                            |
| Object storage        | `s3.bucket.created`, `s3.bucket.updated`, `s3.bucket.deleted`, `s3.bucket.quota.set`, `s3.bucket.versioning.set`, `s3.bucket.lifecycle.set`, `s3.bucket.object_lock.set`, `s3.object.deleted`, `s3.credential.minted`, `s3.credential.revoked`, `s3.presign.issued`                                                      |
| Billing               | `billing.balance.low`, `billing.balance.topped_up`, `billing.balance.charged`, `billing.balance.refunded`, `billing.payment.succeeded`, `billing.payment_method.added`, `billing.subscription.activated`, `billing.subscription.canceled`, `billing.promo_code.redeemed`, `billing.plan.created`, `billing.plan.updated` |

The event registry also declares `compute.snapshot.pruned`, `s3.lifecycle.transitioned`, `dns.domain.dnssec.toggled`, `plugin.installed`, `plugin.enabled`, `plugin.disabled`, `plugin.deleted`, `auth.user.registered`, `auth.user.login` and `auth.user.logout`, but nothing emits them yet.

Each event carries a topic, the tenant, the actor, the resource id and a free-form JSON metadata blob. Because handler exports receive no arguments today, plugins cannot read these fields yet.
