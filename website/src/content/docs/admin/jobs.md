---
title: Background jobs
description: The River job queue that runs Lahijan's background work, the job kinds it knows, the jobs settings and how to inspect, retry and cancel jobs.
---

Lahijan runs background work on a durable job queue built on [River](https://riverqueue.com), which stores jobs in the same PostgreSQL database as everything else. There is no Redis or separate broker. This page lists the job kinds, explains the settings, and shows how to inspect, retry and cancel jobs.

## Turn the queue on

The queue is off by default in the config file and on in the production compose file:

```ini title=".env"
LAHIJAN_JOBS_ENABLED=true
LAHIJAN_JOBS_ADMINUI_ENABLED=true
```

When `jobs.enabled` is `false`, the queue is not built, the jobs API returns `501 not_implemented`, and features that depend on it (plugin invocations, scheduled plugin jobs) do nothing.

## Settings

| Key                              | Default          | Meaning                                                                                                                       |
| -------------------------------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `jobs.enabled`                   | `false`          | Build and start the queue.                                                                                                    |
| `jobs.maxAttempts`               | `5`              | Retry budget for jobs that do not set their own.                                                                              |
| `jobs.jobTimeoutSeconds`         | `60`             | Time limit for one job run, unless a worker sets its own.                                                                     |
| `jobs.softStopTimeoutSeconds`    | `30`             | On shutdown, how long to wait for running jobs before cancelling them.                                                        |
| `jobs.defaultMaxWorkersPerQueue` | `10`             | Concurrent workers per queue. Raise it on bigger hosts.                                                                       |
| `jobs.pollOnly`                  | `false`          | Poll for new jobs instead of using PostgreSQL LISTEN/NOTIFY. Set `true` behind PgBouncer in transaction pooling mode.         |
| `jobs.queues`                    | `{}`             | Intended per-queue worker overrides. The server does not read this key in this release; use `jobs.defaultMaxWorkersPerQueue`. |
| `jobs.adminUI.enabled`           | `true`           | Mount River's built-in web UI.                                                                                                |
| `jobs.adminUI.path`              | `/admin/jobs/ui` | Where the UI is mounted.                                                                                                      |

See [Configuration](/docs/reference/configuration) for how keys map to environment variables.

## Job kinds

Each job has a kind, which selects the worker, and a queue. These kinds are registered:

| Kind                         | Queue           | What it does                                                                                                                                | Registered when        |
| ---------------------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| `billing.meter.collect`      | `billing`       | Collects usage from the compute, DNS and object storage backends and appends usage events.                                                  | Always (with jobs on)  |
| `billing.usage.rollup`       | `billing`       | Rolls usage events up into ledger charges.                                                                                                  | Always                 |
| `billing.balance.check`      | `billing`       | Finds tenants at zero balance and stops instances past the grace period.                                                                    | Always                 |
| `billing.receipt.generate`   | `billing`       | Generates a receipt PDF for a user and period.                                                                                              | Always                 |
| `auditlog.prune`             | `maintenance`   | Audit log pruning per retention policy.                                                                                                     | Always                 |
| `notify.email.send`          | `notifications` | Sends an email asynchronously.                                                                                                              | Always                 |
| `compute.snapshot.take`      | `compute`       | Scans snapshot policies that are due and takes snapshots in Incus.                                                                          | Compute enabled        |
| `compute.snapshot.prune`     | `compute`       | Deletes expired snapshots.                                                                                                                  | Compute enabled        |
| `compute.backup.create`      | `compute`       | Exports a snapshot and pushes it to a backup target.                                                                                        | Compute enabled        |
| `storage.lifecycle.evaluate` | `storage`       | Applies bucket lifecycle rules to objects in SeaweedFS. Runs one at a time.                                                                 | Object storage enabled |
| `wasm.plugin.invoke`         | `plugins`       | Calls a plugin export, for event deliveries and plugin-scheduled jobs. See [How plugins work](/docs/plugins/overview#how-plugin-code-runs). | Plugins enabled        |

> [!WARNING]
> Several of these workers are described as periodic, but this release does not register a timer for any of them. Nothing enqueues `billing.meter.collect`, `billing.balance.check`, the snapshot jobs, `auditlog.prune` or `storage.lifecycle.evaluate` automatically. `compute.backup.create` is queued by the snapshot worker, and `wasm.plugin.invoke` by events and plugins.

> [!WARNING]
> The queue list is built when the job client is created, from the workers registered at that moment. The billing, compute, storage and plugin workers are registered afterwards, so their queues (`billing`, `compute`, `storage`, `plugins`) may not be picked up by any worker. If jobs in those queues stay `available`, this is the cause. Check with the API below.

## Job states

| State       | Meaning                                             |
| ----------- | --------------------------------------------------- |
| `available` | Ready to run.                                       |
| `scheduled` | Waiting for its scheduled time.                     |
| `pending`   | Not yet ready to be picked up.                      |
| `running`   | A worker has it.                                    |
| `retryable` | Failed; will be retried after a backoff.            |
| `completed` | Finished successfully.                              |
| `cancelled` | Cancelled by an admin or a worker.                  |
| `discarded` | Failed and out of attempts (the dead-letter state). |

## Inspect jobs with the API

All endpoints need the `X-Tenant-Id` header and the listed permission. Only the platform admin role holds the `platform.jobs.*` permissions.

| Method and path                          | Permission             | What it does                        |
| ---------------------------------------- | ---------------------- | ----------------------------------- |
| `GET /api/v1/admin/jobs`                 | `platform.jobs.read`   | List jobs, newest first.            |
| `GET /api/v1/admin/jobs/{jobId}`         | `platform.jobs.read`   | One job.                            |
| `POST /api/v1/admin/jobs/{jobId}/retry`  | `platform.jobs.retry`  | Re-queue a `discarded` job.         |
| `POST /api/v1/admin/jobs/{jobId}/cancel` | `platform.jobs.cancel` | Cancel a job that has not finished. |

List filters:

| Parameter | Meaning                                                                                  |
| --------- | ---------------------------------------------------------------------------------------- |
| `state`   | One or more states; repeat the parameter to OR them (`state=discarded&state=retryable`). |
| `kind`    | Exact kind, for example `billing.usage.rollup`.                                          |
| `queue`   | Queue name.                                                                              |
| `limit`   | Page size, default 25, max 200.                                                          |

> [!NOTE]
> The list accepts `offset` but does not apply it: every call returns the newest `limit` jobs that match. `total` is the offset plus the number of items on the page, not a count of all jobs. Narrow the result with filters instead of paging.

```sh
curl -s "https://lahijan.example.com/api/v1/admin/jobs?state=discarded&limit=50" \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID"
```

Each job has `id`, `kind`, `state`, `queue`, `priority`, `attempt`, `maxAttempts`, `createdAt`, `scheduledAt`, `attemptedAt`, `finalizedAt`, `attemptedBy`, `tags`, `args`, `metadata` and `errors` (one entry per failed attempt).

### Retry and cancel

- **Retry** only works on `discarded` jobs. Any other state returns `409 conflict`. The job is re-queued to run now.
- **Cancel** works on `available`, `scheduled`, `pending`, `retryable` and `running` jobs. `completed`, `cancelled` and `discarded` jobs return `409 conflict`.

Both actions are recorded in the [audit log](/docs/audit/overview) as `platform.job.retry` and `platform.job.cancel`, with the job id and the state before and after.

## River web UI

When `jobs.adminUI.enabled` is `true`, River's own web UI and its API are mounted at `jobs.adminUI.path` (`/admin/jobs/ui` by default), behind the `platform.jobs.read` permission. The dashboard does not link to it.

> [!NOTE]
> That permission check needs a tenant scope, taken from the `X-Tenant-Id` header or a `tenant_id` query parameter. A plain browser visit sends neither and is refused with `400 tenant_scope_required`. Use the REST API above if you cannot add the header, for example with a browser extension or a reverse proxy rule.
