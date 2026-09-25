---
title: Audit log
description: What the audit log records, why its rows cannot be changed, and how to filter, inspect and export events.
---

The audit log is the record of privileged actions in a tenant: who created, changed or deleted which resource, when, and whether it worked. Open it from **Audit Log** in the sidebar, or read it through the API.

## What gets recorded

Each event has:

| Field       | Meaning                                                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------------------------------------ |
| Action      | What was done, as `scope.action`, for example `compute.instance.create`, `dns.record.delete` or `billing.balance.topup`. |
| Actor       | Who did it: the user id, and the actor type `user`, `system` or `plugin`.                                                |
| Resource    | The resource type (for example `instance`) and its id.                                                                   |
| Status      | `success`, `failure` or `pending`.                                                                                       |
| Request id  | The id of the HTTP request, useful when you report a problem to your operator.                                           |
| Metadata    | Extra details, such as the name of a new instance or the amount of a top-up.                                             |
| Recorded at | When it happened (UTC).                                                                                                  |

Most actions are written **before** they run, with the status `pending`, and get an outcome (`success` or `failure`) once they finish. If the action crashes halfway, the pending row still shows that it was attempted. The event's status is its latest outcome; the detail view lists every outcome in order.

Compute, DNS, object storage, billing and plugin actions in the tenant appear here. Exporting the audit log is itself recorded (`audit.export`).

> [!NOTE]
> Account events (sign-in, sign-out, password and token changes, second-factor changes, linked identities) and [Agent](/docs/agent/overview) events are recorded without a tenant. They are kept, but the tenant list and export do not include them in the current release.

## Rows cannot be changed

The audit log is append-only. The database rejects every attempt to update or delete an audit row or an outcome row, including attempts by Lahijan itself. A status change is recorded by adding an outcome row, never by editing the original.

There is no retention limit in this release: events are kept until your operator removes them at the database level.

## Browse and filter

The **Audit Log** page lists events newest first, 50 at a time, with **Previous** and **Next** to page through them. Columns are **Action**, **Actor**, **Resource**, **Status** and **At**.

Narrow the list with the filters:

| Filter              | What it matches                                          |
| ------------------- | -------------------------------------------------------- |
| **Action slug**     | One exact action, for example `compute.instance.create`. |
| **Status**          | **All**, **Success**, **Failure** or **Pending**.        |
| **Actor type**      | **All**, **User**, **System** or **Plugin**.             |
| **Resource type**   | One resource type, for example `instance`.               |
| **Actor user id**   | Events by one user (a UUID).                             |
| **From** and **To** | A date range.                                            |

Filters apply as you type. Select **View detail** on a row to see every field, the metadata and the outcome trail.

## The Audit tab on an instance

Each instance's detail page (**Instances**, then the instance) has an **Audit** tab. It shows the last 100 events for that instance: the same data as the audit log, filtered to the resource type `instance` and the instance's id.

## Export

Select **Export**, then **Export as CSV** or **Export as JSON**. The export uses the filters you have set, has no paging and stops at 10,000 rows.

> [!WARNING]
> In the current release the dashboard's export links do not send the tenant, so the download fails with a `tenant_scope_required` error. Use the API call below instead.

The CSV has these columns: `id`, `tenant_id`, `actor_user_id`, `actor_type`, `action`, `resource_type`, `resource_id`, `status`, `request_id`, `metadata`, `created_at`. The JSON export is an array of the same event objects the list returns.

## Use the API

| Method and path               | Permission     | Purpose                             |
| ----------------------------- | -------------- | ----------------------------------- |
| `GET /api/v1/audit`           | `audit.read`   | A page of events.                   |
| `GET /api/v1/audit/{auditId}` | `audit.read`   | One event with its outcome trail.   |
| `GET /api/v1/audit/export`    | `audit.export` | All matching events as CSV or JSON. |

`GET /api/v1/audit` accepts `limit` (1 to 200, default 50), `offset`, and these filters: `actorUserId`, `action`, `resourceType`, `resourceId`, `status` (`success`, `failure`, `pending`), `actorType` (`user`, `system`, `plugin`), `fromTs` and `toTs` (RFC 3339). The response has `items`, `total`, `limit` and `offset`.

```sh
curl "https://cloud.example.com/api/v1/audit?action=compute.instance.delete&status=failure" \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID"
```

`GET /api/v1/audit/export` takes the same filters except `resourceId`, has no `limit` or `offset`, and adds `format` (`csv`, the default, or `json`):

```sh
curl -o audit-export.csv \
  "https://cloud.example.com/api/v1/audit/export?format=csv&fromTs=2026-09-01T00:00:00Z" \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID"
```

`GET /api/v1/audit/{auditId}` also returns an account-level event (one without a tenant) if you know its id.

## Who can read it

`audit.read` is held by every built-in tenant role: owner, administrator, member and viewer. `audit.export` is held by owners and administrators. See [Roles and permissions](/docs/admin/roles-and-permissions).
