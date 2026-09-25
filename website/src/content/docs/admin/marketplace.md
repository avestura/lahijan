---
title: Plugin marketplace
description: Configure a plugin marketplace index, then install and upgrade plugins from it in the dashboard or the admin API.
---

A marketplace is a YAML index of plugins that admins can install with one click, instead of uploading `.lahx` files by hand. It also gives you the only in-place upgrade path: upgrading from the marketplace replaces the installed version and carries its grants forward. This page covers configuring an index, the index format, and installing and upgrading from it.

## Configure the index source

The marketplace reads its index from a local directory or an HTTP(S) URL, set in the `wasm.marketplace` block:

| Key                                | Default                        | Meaning                                                                                           |
| ---------------------------------- | ------------------------------ | ------------------------------------------------------------------------------------------------- |
| `wasm.marketplace.path`            | `examples/plugins/marketplace` | A directory, relative to the Lahijan working directory, that contains `plugins-marketplace.yaml`. |
| `wasm.marketplace.url`             | empty                          | An HTTP(S) URL. When set, it wins over `path`.                                                    |
| `wasm.marketplace.cacheTtlSeconds` | `60`                           | How long the parsed index is cached.                                                              |

```yaml
wasm:
  enabled: true
  marketplace:
    path: "/etc/lahijan/marketplace"
    url: ""
    cacheTtlSeconds: 60
```

As environment variables: `LAHIJAN_WASM_MARKETPLACE_PATH`, `LAHIJAN_WASM_MARKETPLACE_URL`, `LAHIJAN_WASM_MARKETPLACE_CACHETTLSECONDS`.

The marketplace is only active when `wasm.enabled` is `true`. When both `path` and `url` are empty, the marketplace endpoints return `501 not_implemented`.

> [!WARNING]
> The production container image does not include the repository's `examples/` directory, so the default `path` points at nothing and listing the marketplace fails with `500 internal`. In the Docker Compose deployment, mount an index directory into the container and set `LAHIJAN_WASM_MARKETPLACE_PATH`, or set `LAHIJAN_WASM_MARKETPLACE_URL`.

### Local directory

The local loader re-reads the file when its modification time changes or the cache expires. Plugin files are read from `<path>/<source.path>/`.

### Remote URL

The HTTP loader fetches `url` and expects the raw YAML as the response body (at most 1 MiB, 10 second timeout). It re-fetches only when the cache expires.

The same `url` is also the base for plugin files. For an entry with `source.path: slack-notifier`, Lahijan fetches `<url>/slack-notifier/plugin.lahx`, and if that returns 404, `<url>/slack-notifier/lahijan.manifest.yaml` and `<url>/slack-notifier/plugin.wasm`. Serve your index so that both the index and these paths resolve from the one URL, for example a directory URL whose index document is the YAML file.

## Index format

The file is named `plugins-marketplace.yaml`:

```yaml title="plugins-marketplace.yaml"
version: 1
updated_at: "2026-07-18T00:00:00Z"
plugins:
  - name: slack-notifier
    version: 1.0.0
    description: "Posts compute.instance.* events to a Slack webhook."
    author: "Lahijan Maintainers <lahijan@example.dev>"
    license: Apache-2.0
    homepage: "https://github.com/avestura/lahijan/tree/main/examples/plugins/slack-notifier"
    permissions:
      - "events.listen:compute.instance.*"
      - "network.outbound"
      - "config.read"
    source:
      repo: local
      path: slack-notifier
    sha256: "3f5a..."
```

| Field                                                    | Required | Meaning                                                                                  |
| -------------------------------------------------------- | -------- | ---------------------------------------------------------------------------------------- |
| `version`                                                | yes      | Index format version. Must be non-zero.                                                  |
| `updated_at`                                             | no       | Informational timestamp.                                                                 |
| `plugins`                                                | yes      | At least one entry. Names must be unique.                                                |
| `plugins[].name`                                         | yes      | Plugin name; should match the manifest name.                                             |
| `plugins[].version`                                      | yes      | The version the index offers.                                                            |
| `plugins[].description`, `author`, `license`, `homepage` | no       | Shown to admins.                                                                         |
| `plugins[].permissions`                                  | no       | Shown to admins. The permissions actually requested come from the plugin's own manifest. |
| `plugins[].source.repo`                                  | yes      | `local` or `git`.                                                                        |
| `plugins[].source.path`                                  | no       | Subdirectory holding the plugin files. Defaults to the entry name.                       |
| `plugins[].source.git_url`, `git_ref`                    | no       | Reserved for `git` sources.                                                              |
| `plugins[].sha256`                                       | no       | SHA-256 (hex) of `plugin.wasm`. Checked on install.                                      |

Each plugin directory holds either `plugin.lahx` (preferred) or the two loose files `lahijan.manifest.yaml` and `plugin.wasm`.

> [!CAUTION]
> Always set `sha256`. It pins the module bytes: on install, Lahijan hashes the module and refuses a mismatch with `422`. When `sha256` is empty or the placeholder `REPLACE_ME_AFTER_BUILD`, the check is skipped and any bytes at the source are installed. The in-repository example index still uses the placeholder.

Compute the hash of the module (not of the `.lahx` file):

```sh
sha256sum my-plugin/plugin.wasm
```

`git` sources are not supported yet: the remote loader refuses them, and the local loader ignores `repo` and reads from the directory.

## Install a plugin

1. Open **Administration > Marketplace**. The table lists **Name**, **Version**, **Description** and **License** for each entry.
2. Click **Install** on a plugin that is not installed yet.
3. Confirm in the **Install {name}?** dialog.

Lahijan fetches the package, verifies the hash, validates the manifest and compiles the module, exactly as for an upload. The plugin lands in **Pending** in the tenant you have selected. Then open **Administration > Plugins**, grant its permissions and enable it, as described in [Installing plugins](/docs/admin/plugins#review-and-grant-permissions).

## Upgrade a plugin

When an installed plugin's version differs from the index version, the row shows **Upgrade**. Click it and confirm the **Upgrade {name}?** dialog.

The upgrade:

1. Refuses if the index version is the same as, or lower than, the installed one. Downgrades are not supported; delete the plugin and install the older version instead.
2. Installs the new version in **Pending**.
3. Copies each existing grant that the new manifest still requests (exact match). Grants it no longer requests are dropped.
4. Deletes the old version with its routes, subscriptions, key-value data and config.

The response lists `preservedGrants`, `droppedGrants` and `newPermissions`. Grant the new permissions, then enable the new version: it does not become active on its own.

> [!NOTE]
> The upgrade compares against the most recent installed version of that name in the scope of the request. It uses semantic version numbers and ignores pre-release suffixes, so `1.0.0-rc1` and `1.0.0` count as the same version.

## Admin API

All endpoints need an authenticated user and the `X-Tenant-Id` header.

| Method and path                             | Permission        | What it does                                                                                                                                                                                      |
| ------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/admin/marketplace`             | `plugins.read`    | The index entries, as `{items: [...]}`.                                                                                                                                                           |
| `GET /api/v1/admin/marketplace/{name}`      | `plugins.read`    | One entry. `404` if the name is not in the index.                                                                                                                                                 |
| `POST /api/v1/admin/plugins/install/{name}` | `plugins.install` | Install from the index. Returns `201` and the plugin in `pending`. `409` if that name and version is already installed.                                                                           |
| `POST /api/v1/admin/plugins/upgrade/{name}` | `plugins.install` | Upgrade to the index version. Returns `{plugin, oldId, preservedGrants, droppedGrants, newPermissions}`. `404` if the plugin is not installed, `409` for the same version, `400` for a downgrade. |

Entries are returned with the fields `name`, `version`, `description`, `author`, `license`, `homepage`, `permissions`, `source` (`repo`, `path`, `gitURL`, `gitRef`) and `sha256`.

Marketplace installs and upgrades are recorded in the [audit log](/docs/audit/overview) as `plugins.marketplace_install` and `plugins.marketplace_upgrade`, in addition to the regular `plugins.upload` and `plugins.upgrade` entries.
