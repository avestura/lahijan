---
title: Snapshots and backups
description: Take, restore and delete instance snapshots, and learn what snapshot policies, backup targets and off-host backups can and cannot do today.
---

A snapshot records the state of an instance at a point in time so you can roll back to it later. Snapshots are fully supported in the dashboard and the API. Snapshot policies, backup targets and off-host backups exist in the API but are only partly working in this version; the sections below say exactly what you can rely on.

## Snapshots

### Take a snapshot

1. Open the instance and select the **Snapshots** tab.
2. Enter a **Snapshot name** (for example `pre-deploy`) and, if you like, a **Description (optional)**.
3. Tick **Capture runtime state (stateful)** if you also want the memory and running processes saved, not only the disks.
4. Click **Create snapshot**.

The snapshot appears in the list, which refreshes every 10 seconds. Each entry shows whether it was taken **manual** or **by schedule**, whether it is stateful, its **Size**, when it was **Taken** and, if set, when it **Expires**.

Snapshot names must be unique per instance; reusing a name returns `409`. Stateful snapshots depend on support in the underlying system and may not be available on every deployment; if the server refuses one, you see **Failed to create snapshot.**

With the API:

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID/snapshots \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "name": "pre-deploy", "description": "Before the cut-over", "stateful": false }'
```

`name` is required (1 to 63 characters). The response is `201` with the snapshot object.

### List and inspect snapshots

- `GET /api/v1/compute/instances/{instanceId}/snapshots` returns a page of snapshots (`limit` 1 to 200, `offset`).
- `GET /api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}` returns one snapshot.

### Restore a snapshot

Click **Restore** next to a snapshot, then **Confirm**. The instance's current state is replaced by the snapshot's.

> [!WARNING]
> Restoring discards every change made since the snapshot was taken. Take a fresh snapshot first if you might need the current state.

The dashboard restores the disk state only. To restore the runtime state of a stateful snapshot, use the API and set `stateful`:

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID/snapshots/$SNAPSHOT_ID/restore \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "stateful": true }'
```

The request body is optional; without it, `stateful` is `false`.

### Delete a snapshot

Click the trash icon next to a snapshot, then **Confirm**. With the API, send `DELETE /api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}`, which returns `204`. Deleted snapshots cannot be recovered.

### Snapshot permissions

| Action        | Permission                | Default roles                |
| ------------- | ------------------------- | ---------------------------- |
| List and view | `compute.snapshot.read`   | Viewer, Member, Admin, Owner |
| Take          | `compute.snapshot.create` | Member, Admin, Owner         |
| Restore       | `compute.instance.update` | Member, Admin, Owner         |
| Delete        | `compute.snapshot.delete` | Admin, Owner                 |

## Snapshot policies

A snapshot policy describes a schedule: how often to snapshot, how many snapshots to keep, and optionally a backup target to copy them to. Policies are managed through the API only; there is no dashboard page.

> [!WARNING]
> In this version, policies are stored and validated, but the server does not yet run them on a timer, and the `retainCount` limit is not enforced. Do not rely on a policy as your only protection. Take manual snapshots for anything important.

When a policy does run, it takes a snapshot named `auto-<unix seconds>` of its instance. A policy without an `instanceId` is a tenant-wide default and covers every instance in the tenant.

Create a policy:

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/snapshot-policies \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "nightly-web-1",
    "instanceId": "8a1d6c3e-5b7f-4e2a-9c0d-1f2e3a4b5c6d",
    "cadence": "P1D",
    "retainCount": 7,
    "enabled": true
  }'
```

| Field         | Required | Meaning                                                                                                                                                              |
| ------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`        | yes      | 1 to 63 characters, unique in the tenant.                                                                                                                            |
| `cadence`     | yes      | An ISO 8601 duration made of weeks, days, hours and minutes, such as `PT1H`, `P1D`, `P1W` or `P1DT2H`. Other forms (months, years, seconds) are rejected with `400`. |
| `instanceId`  | no       | The instance to snapshot. Leave it out for a tenant-wide policy.                                                                                                     |
| `retainCount` | no       | How many snapshots to keep. Default `7`.                                                                                                                             |
| `targetId`    | no       | A backup target to copy each snapshot to.                                                                                                                            |
| `enabled`     | no       | Default `true`.                                                                                                                                                      |

Other operations:

- `GET /api/v1/compute/snapshot-policies` lists policies; `GET /api/v1/compute/snapshot-policies/{policyId}` returns one, with `lastRunAt` and `nextRunAt`.
- `PATCH /api/v1/compute/snapshot-policies/{policyId}` replaces the policy. `name` and `cadence` are required in the body.
- `DELETE /api/v1/compute/snapshot-policies/{policyId}` deletes it.

Reading needs `compute.snapshot_policy.read` (every default role). Creating, updating and deleting need `compute.snapshot_policy.create`, `.update` and `.delete`, which only Admin and Owner have.

## Backup targets

A backup target is a place outside the compute hosts where snapshots can be copied: an S3-compatible bucket (`s3`), a mounted network share (`nfs`), or a server reached over SSH (`ssh`). Targets are managed through the API only.

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/backup-targets \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "offsite-s3",
    "kind": "s3",
    "description": "Nightly copies",
    "config": {
      "endpoint": "https://s3.example.net",
      "bucket": "instance-backups",
      "region": "us-east-1",
      "prefix": "lahijan/",
      "force_path_style": true
    },
    "secret": {
      "s3_access_key_id": "AKIA...",
      "s3_secret_key": "..."
    },
    "enabled": true
  }'
```

`name`, `kind`, `config` and `secret` are required. The keys each kind understands:

| Kind  | `config` keys                                                 | `secret` keys                                 |
| ----- | ------------------------------------------------------------- | --------------------------------------------- |
| `s3`  | `endpoint`, `bucket`, `region`, `prefix`, `force_path_style`  | `s3_access_key_id`, `s3_secret_key`           |
| `nfs` | `mount_point`, `sub_path`                                     | `nfs_password`                                |
| `ssh` | `host`, `port`, `user`, `remote_path`, `host_key_fingerprint` | `ssh_private_key`, `ssh_private_key_password` |

The `secret` object is encrypted at rest and never returned by the API. For `nfs`, the share must already be mounted on the Lahijan server by your operator.

List targets with `GET /api/v1/compute/backup-targets`, read one with `GET /api/v1/compute/backup-targets/{targetId}` and remove one with `DELETE /api/v1/compute/backup-targets/{targetId}`. There is no update call; delete and recreate a target to change it. Viewing needs `compute.backup.target.read` (every default role); creating and deleting need `compute.backup.target.create` and `.delete` (Admin and Owner).

## Backups

A backup is a copy of a snapshot stored on a backup target. You can list, inspect and delete backups:

- `GET /api/v1/compute/backups` lists every backup in the tenant.
- `GET /api/v1/compute/instances/{instanceId}/backups` lists the backups of one instance.
- `GET /api/v1/compute/backups/{backupId}` returns one backup with its `status` (`pending`, `uploading`, `completed` or `failed`), `sizeBytes`, `checksumSha256`, `remoteLocation` and `errorMessage`.
- `DELETE /api/v1/compute/backups/{backupId}` removes the stored copy from the target and deletes the record. Needs `compute.backup.delete` (Admin and Owner).

> [!NOTE]
> There is no API call to start a backup on demand or to restore an instance from a backup. Backups are only produced by snapshot policies that name a `targetId`, and policies do not run on a timer in this version. For now, treat snapshots as your recovery mechanism, and ask your operator about [platform backups](/docs/operations/backups).
