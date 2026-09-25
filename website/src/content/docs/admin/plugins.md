---
title: Installing plugins
description: Upload a .lahx plugin package, approve its permissions, enable, disable and uninstall it from the dashboard or the admin API.
---

As an admin you decide which plugins run on your Lahijan and what each one may do. This page covers uploading a package, reviewing and granting permissions, turning plugins on and off, and removing them. For background on the sandbox and permission model, read [How plugins work](/docs/plugins/overview).

## Before you start

- The plugin runtime must be on: `wasm.enabled: true`. The production compose file sets `LAHIJAN_WASM_ENABLED=true`. When it is off, the plugin endpoints return `501 not_implemented`.
- Plugin code runs through the job queue, so `jobs.enabled` should also be `true` (the production compose file sets it).
- You need the **Administration** section of the dashboard, which platform admins see. Through the API, the plugin endpoints need the `plugins.*` permissions below.
- You need a `.lahx` package. See [Packaging (.lahx)](/docs/plugins/packaging).

A plugin is installed into the tenant selected in the tenant switcher when you upload it (the `X-Tenant-Id` of the request). Its compute, DNS and storage host functions act in that tenant.

## Upload a plugin

1. Open **Administration > Plugins**.
2. Click **Upload plugin**. The **Upload extension** dialog opens.
3. Drop a `.lahx` file on the **Extension package** area, or click it to browse. Only files ending in `.lahx` are accepted; anything else shows "Choose a .lahx extension package."
4. Click **Upload**.

Lahijan unpacks the archive, validates the manifest, compiles the module and saves the plugin in the **Pending** state. It is not running yet.

Common errors:

| Response                                 | Cause                                                                                                       |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `400 bad_request` with manifest messages | The manifest failed validation, for example an unknown permission.                                          |
| `400 bad_request` with `details.cause`   | The module did not compile or declares more memory than `wasm.maxMemoryPerPlugin`.                          |
| `400 bad_request` with `details.reason`  | The archive is malformed (extra files, nested paths, encrypted entries).                                    |
| `409 conflict`                           | A plugin with the same name and version is already installed.                                               |
| `413 payload_too_large`                  | The package or module exceeds its limit, or the request exceeds `http.server.bodylimit` (4 MiB by default). |

## Review and grant permissions

Open the plugin from the list (**View detail** in the row menu, or click its name). The **Permissions** card lists every permission from the manifest, each marked **Requested** or **Granted**.

- Click **Grant** next to a permission to approve it.
- Click **Revoke** to take it back. The change applies on the plugin's next host call.

Grant only what you understand. Things to check:

- `network.outbound` lets the plugin send HTTP requests to any address the server can reach, including internal ones. There is no per-plugin URL allowlist.
- `compute.*`, `dns.*` and `storage.*` let it create and delete real resources in its tenant, billed to that tenant.
- `events.listen:<pattern>` subscriptions are not filtered by tenant; the plugin receives matching events from all tenants.
- `*` exposes the full WASI surface. It does not grant any Lahijan host function.

> [!NOTE]
> The key-value, config and outbound HTTP functions check the unqualified permission. If a manifest requests `network.outbound:hooks.slack.com`, granting it does not allow outbound calls. The plugin needs `network.outbound`. You can grant a permission that is not in the manifest through the API (below), as long as it is in the catalog. See the [Manifest reference](/docs/plugins/manifest#permissions).

The detail page also has a **Manifest** card with the stored manifest as JSON, and a **Plugin detail** card with the plugin id, name, version, tenant, **WASM hash** (SHA-256 of the module) and **WASM size**.

## Enable and disable

- Click **Enable** on the detail page or in the list to move the plugin to **Active**.
- Click **Disable** to move it to **Disabled**. Grants are kept for when you enable it again.

Enabling does not require grants; a plugin with none runs, but every host call is denied.

> [!WARNING]
> In this release the job worker that runs plugin code does not check the plugin's status. Disabling alone does not stop already-registered subscriptions or scheduled jobs from firing. To stop a plugin completely, revoke its permissions or delete it. See [current limitations](/docs/plugins/overview#current-limitations).

## Uninstall

Click **Delete** on the detail page or in the row menu. The dashboard deletes immediately without a confirmation step. Deleting removes the plugin and all its state: grants, key-value data, config, event subscriptions and registered routes. It cannot be undone.

## Upgrade

Uploading a new version of a plugin that is already installed creates a second, separate plugin row. To replace a version in place and carry its grants forward, install it from a marketplace and click **Upgrade**. See [Plugin marketplace](/docs/admin/marketplace#upgrade-a-plugin).

## Logs and troubleshooting

There is no per-plugin log screen. To see what a plugin does:

- The [audit log](/docs/audit/overview) records every admin action on plugins with resource type `plugin`.
- Each host call produces an OpenTelemetry span named `wasm.host.<module>.<function>` with the plugin id, the permission checked and the result (`denied`, `not_found`, and so on). See [Observability](/docs/operations/observability).
- Plugin invocations are jobs of kind `wasm.plugin.invoke` on the `plugins` queue. Failed runs and their errors are visible through [Background jobs](/docs/admin/jobs).

## Admin API

All endpoints need an authenticated user and the `X-Tenant-Id` header. See [REST API](/docs/reference/api) for authentication.

| Method and path                                                           | Permission                   | What it does                                                                                                                                                         |
| ------------------------------------------------------------------------- | ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/admin/plugins`                                               | `plugins.install`            | List plugins across all tenants. Query `limit` (default 50, max 200) and `offset`. Returns `{items, total, limit, offset}`; list items omit the manifest and grants. |
| `POST /api/v1/admin/plugins/upload`                                       | `plugins.install`            | Multipart upload with one part named `package` holding the `.lahx` file. Returns `201` and the plugin in `pending`.                                                  |
| `GET /api/v1/admin/plugins/{pluginId}`                                    | `plugins.install`            | One plugin with its manifest and granted permissions.                                                                                                                |
| `POST /api/v1/admin/plugins/{pluginId}/permissions/{permission}/{action}` | `plugins.permission.approve` | `action` is `grant` or `revoke`. Returns the updated list of granted permissions. URL-encode the permission (`:` is `%3A`, `*` is `%2A`).                            |
| `POST /api/v1/admin/plugins/{pluginId}/enable`                            | `plugins.install`            | Set status to `active`.                                                                                                                                              |
| `POST /api/v1/admin/plugins/{pluginId}/disable`                           | `plugins.install`            | Set status to `disabled`.                                                                                                                                            |
| `DELETE /api/v1/admin/plugins/{pluginId}`                                 | `plugins.uninstall`          | Delete the plugin and its state.                                                                                                                                     |

The platform admin role passes every check. The seeded tenant owner and tenant admin roles also hold the `plugins.*` permissions in their tenant. See [Roles and permissions](/docs/admin/roles-and-permissions).

A full install from a script:

```sh
BASE=https://lahijan.example.com
AUTH="Authorization: Bearer $LAHIJAN_TOKEN"
TENANT="X-Tenant-Id: $TENANT_ID"

PLUGIN_ID=$(curl -s -X POST "$BASE/api/v1/admin/plugins/upload" \
  -H "$AUTH" -H "$TENANT" -F "package=@my-plugin-0.1.0.lahx" | jq -r .id)

for perm in 'events.listen%3Acompute.instance.stopped' 'kv.read' 'kv.write'; do
  curl -s -X POST "$BASE/api/v1/admin/plugins/$PLUGIN_ID/permissions/$perm/grant" \
    -H "$AUTH" -H "$TENANT"
done

curl -s -X POST "$BASE/api/v1/admin/plugins/$PLUGIN_ID/enable" -H "$AUTH" -H "$TENANT"
```

The plugin object has these fields: `id`, `tenantId`, `name`, `version`, `description`, `wasmHash`, `wasmSize`, `status` (`pending`, `active` or `disabled`), `manifest`, `permissions`, `createdAt` and `updatedAt`.
