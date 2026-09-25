---
title: Instances
description: Create, start, stop, edit and delete system containers and virtual machines from the dashboard or the API.
---

An instance is a system container or a virtual machine running in your tenant. This page covers the full life of an instance: creating it, starting and stopping it, changing its configuration and devices, and deleting it.

## Create an instance

### In the dashboard

1. Go to **Instances** and click **New instance**.
2. **Image** step: pick an image from the tenant catalog, or type an alias such as `ubuntu/24.04`. **Browse** opens the public image catalog so you can search for one. See [Images](/docs/compute/images).
3. **Size** step: fill in the details.
   - **Name**: 1 to 63 characters, lowercase letters, numbers and hyphens, starting with a letter or number. Names are unique within the tenant.
   - **Type**: **Container** or **Virtual Machine**.
   - **vCPUs** (1 to 64), **Memory (MiB)** (64 to 65536) and **Disk (GiB)**.
   - **Description** (optional).
   - **Advanced options** lets you pick a **Profile**. The list shows profiles created in your tenant; when it says **No profiles available.** the instance uses the `default` profile.
4. **Review** step: check the summary and click **Create instance**.

The dashboard opens the new instance's page. New instances are created in the **Stopped** state, so click **Start** to boot it.

> [!NOTE]
> In this version two wizard fields are not applied. The **Disk (GiB)** value is dropped, and the instance gets the root disk from the `default` profile, which has no size limit. The **Config (YAML/JSON)** box under **Advanced options** is not sent to the server. Set extra configuration after creation in the **Config** tab, or create the instance through the API.

### With the API

`POST /api/v1/compute/instances` creates an instance. Only `name` and `imageAlias` are required. `type` defaults to `container` and `profiles` defaults to `["default"]`.

```sh
curl -X POST https://lahijan.example.com/api/v1/compute/instances \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-1",
    "type": "container",
    "imageAlias": "debian/12",
    "description": "Public web server",
    "config": { "limits.cpu": "2", "limits.memory": "2048MiB" },
    "devices": {
      "root": { "type": "disk", "path": "/", "pool": "default", "size": "20GiB" }
    },
    "profiles": ["default"]
  }'
```

Every API call needs a personal access token in the `Authorization: Bearer` header (see [Access tokens](/docs/account/access-tokens)) and your tenant ID in the `X-Tenant-Id` header. Without the tenant header, compute calls fail with `400`.

The response is `201` with the instance object, including its `id`. A few things to know:

- A `root` device only takes effect when it names a `pool`. A `root` device without `pool` is ignored and the profile's root disk is used instead. Ask your operator for the pool name if `default` does not work.
- `limits.cpu`, `limits.memory` and the root `size` count toward your [quota](/docs/compute/overview#quotas). Over quota returns `422` with code `quota_exceeded`.
- A name already in use returns `409`.
- If the creation fails on the server (for example, an unknown image), the name is freed and you can retry.

## List and filter instances

The **Instances** page shows each instance's **Name**, **Status**, **Image** and **Type**. Type in the filter box to match on name, description or image, and use the status dropdown to show **Running**, **Stopped**, **Frozen** or **Other** instances.

Through the API, `GET /api/v1/compute/instances` returns a page of instances. Use `limit` and `offset` to page. The list reflects the last known state; `GET /api/v1/compute/instances/{instanceId}` fetches a single instance and refreshes its status from the live system first.

## Instance states

| Status                                                         | Meaning                                                                                                                                   |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `Running`                                                      | The instance is up.                                                                                                                       |
| `Stopped`                                                      | The instance is shut down. New instances start here.                                                                                      |
| `Frozen`                                                       | All processes are paused. Memory is kept.                                                                                                 |
| `Starting`, `Stopping`, `Restarting`, `Freezing`, `Unfreezing` | A transition is in progress. The dashboard groups these under **Other** and refreshes the page every 2 seconds until the transition ends. |

## Lifecycle actions

The instance page header shows the actions that fit the current state: **Start** when stopped, **Stop** and **Freeze** when running, **Unfreeze** when frozen, and **Restart** and **Delete** at any time. The same actions are in the row menu on the **Instances** list.

The API equivalent is one call per action:

```http
POST /api/v1/compute/instances/{instanceId}/start
POST /api/v1/compute/instances/{instanceId}/stop?force=true&timeout=60
```

`{action}` is one of `start`, `stop`, `restart`, `freeze` or `unfreeze`. Two optional query parameters apply:

- `force` (default `false`): skip the graceful shutdown, like pulling the power cord. The dashboard never sends it for lifecycle actions.
- `timeout` (default `30`): seconds to wait for the action.

The response is the updated instance. Every action is recorded in the audit log.

| Action                       | Permission                 |
| ---------------------------- | -------------------------- |
| `start`                      | `compute.instance.start`   |
| `stop`, `freeze`, `unfreeze` | `compute.instance.stop`    |
| `restart`                    | `compute.instance.restart` |

## Edit configuration and devices

Open the **Config** tab. It has three sections:

- **Effective configuration**: every setting in force after profiles are applied. The **Source** column shows whether a key comes from the **instance**, a **profile** or the **system**. Use the filter box to search.
- **Instance configuration**: the settings stored on this instance, which override profile values. Click **Add setting**, enter a key (for example `limits.cpu`) and a value, then **Save**. **Reset** discards unsaved edits. System-managed keys (`volatile.*`, `image.*`) are hidden and kept as they are.
- **Instance devices**: devices attached directly to the instance. To add one, enter a **Name**, choose a **Type** and write the **Properties** one `key=value` per line, then click **Add device**. For example, a disk device that mounts a storage volume:

  ```ini
  pool=default
  source=my-volume
  path=/mnt/data
  ```

  Remove a device with the trash icon. Devices inherited from profiles are not listed here; they appear in the **Network** and **Storage** tabs.

If the server refuses a change (for example, a blocked device type), you see **Update rejected** with the reason. Some settings apply immediately; others take effect on the next restart.

With the API, `PATCH /api/v1/compute/instances/{instanceId}` accepts `description`, `config`, `devices` and `profiles`. A field you leave out keeps its current value. A field you send replaces the whole map or list, so send every device you want to keep:

```sh
curl -X PATCH https://lahijan.example.com/api/v1/compute/instances/$INSTANCE_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{ "config": { "limits.cpu": "4", "limits.memory": "4096MiB" } }'
```

To see what is in force, including profile values and live usage, call `GET /api/v1/compute/instances/{instanceId}/runtime`.

## Delete an instance

Click **Delete** on the instance page. The dialog asks you to confirm and offers **Force delete (skip graceful shutdown)**. A running instance is stopped first, then removed.

> [!CAUTION]
> Deleting an instance removes it from the system. It cannot be undone. Deleting from the row menu or the bulk action bar on the **Instances** list does not ask for confirmation.

With the API:

```http
DELETE /api/v1/compute/instances/{instanceId}?force=false
```

The response is `204`. Deleting requires `compute.instance.delete`, which the default Member role does not have.

## Bulk actions

Select instances with the checkboxes on the **Instances** list (the header checkbox selects all). A bar appears showing how many are selected, with **Start**, **Stop**, **Restart** and **Delete**. Each button sends one request per selected instance. Only the buttons your role allows are shown.

## Move an instance to another node

On a deployment with several compute nodes, `POST /api/v1/compute/instances/{instanceId}/migrate` moves an instance to another node. The body names the destination in `targetMember`; set `live` to `true` for a live move. Single-node deployments return `409`. See [Compute cluster](/docs/admin/compute-cluster) for how nodes are managed.
