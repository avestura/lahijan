---
title: Storage pools and volumes
description: Understand storage pools, see an instance's disks, and create extra volumes you can attach to instances.
---

Instance disks live in storage pools. A storage pool is a block of disk capacity that the operator sets up on the compute hosts; you cannot create or change pools yourself. Inside a pool you can create volumes: extra disks, separate from an instance's root disk, that you attach to instances and that survive when an instance is deleted.

Pools are managed by the operator. Volumes are managed through the API; the dashboard shows the disks attached to each instance.

## Storage pools

Every instance's root disk comes from a pool. The `default` profile places the root disk on the operator's first storage pool, without a size limit. There is no API to list pools, so ask your operator which pool names you can use. On many installations the pool is called `default`, which is also the pool the volume API uses when you do not name one.

To give a new instance a root disk with a size limit, set a `root` disk device that names the pool when you create it:

```json
{
	"name": "db-1",
	"imageAlias": "debian/12",
	"devices": {
		"root": { "type": "disk", "path": "/", "pool": "default", "size": "40GiB" }
	}
}
```

The `size` of this root device counts toward your tenant's disk [quota](/docs/compute/overview#quotas). A `root` device without `pool` is ignored.

## See an instance's disks

Open the instance and select the **Storage** tab. **Disks** lists every disk in force: the root disk plus any attached volumes or mounts, including ones that come from profiles. For each disk you see the **Mount path**, the **Pool**, the **Size limit** (**no limit** when none is set) and, while the instance is running, how much is **Used**.

## Create a volume

`POST /api/v1/compute/storage` creates a volume. Only `name` is required (1 to 63 characters, unique in the tenant); `poolName` defaults to `default`.

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/storage \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "app-data",
    "description": "Uploads and database files",
    "poolName": "default",
    "config": {}
  }'
```

`config` is a map of string volume settings passed to the compute system as is. The response is `201` with the volume object, including its `id` and `poolName`. A name already in use returns `409`. If the compute system refuses the volume (for example, an unknown pool), the request fails and nothing is saved.

## Attach a volume to an instance

Add a `disk` device that points at the volume. In the dashboard, open the instance's **Config** tab and, under **Instance devices**, add a device:

- **Name**: for example `data`
- **Type**: `disk`
- **Properties**:

  ```ini
  pool=default
  source=app-data
  path=/mnt/data
  ```

`source` is the volume name and `path` is where it appears inside the instance.

With the API, send the device in a `PATCH`. The `devices` map replaces all of the instance's own devices, so include any you already have:

```sh
curl -X PATCH https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "devices": {
      "data": { "type": "disk", "pool": "default", "source": "app-data", "path": "/mnt/data" }
    }
  }'
```

To detach a volume, remove the device with the trash icon under **Instance devices**, or send the `devices` map without it. The volume and its data stay in the pool.

## List, view and delete volumes

- `GET /api/v1/compute/storage` returns a page of volumes (`limit`, `offset`).
- `GET /api/v1/compute/storage/{volumeId}` returns one volume.
- `DELETE /api/v1/compute/storage/{volumeId}` deletes it and returns `204`.

There is no update call.

> [!CAUTION]
> Deleting a volume destroys its data. Detach it from every instance first, and make sure you have a copy of anything you need.

## Permissions

| Action                    | Permission                  | Default roles                |
| ------------------------- | --------------------------- | ---------------------------- |
| List and view volumes     | `compute.storage_pool.read` | Viewer, Member, Admin, Owner |
| Create a volume           | `compute.network.create`    | Admin, Owner                 |
| Attach or detach a volume | `compute.instance.update`   | Member, Admin, Owner         |

Creating a volume is checked against `compute.network.create` in this version, so only tenant admins and owners can create volumes with the default roles.
