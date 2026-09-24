# Default plugin marketplace

This directory is the **default marketplace index** shipped with Lahijan
(WS-10c). Operators may host the index elsewhere (a private Git repo, an
S3 bucket) and point `wasm.marketplace.url` at it; the format is the
same.

## Format

The index is a single YAML file named `plugins-marketplace.yaml`. Each
entry declares:

- `name` — the plugin's unique marketplace name (kebab-case, matches the
  manifest's name).
- `version` — the latest version available (semver, matches the
  manifest's version).
- `description`, `author`, `license`, `homepage` — display-only metadata.
- `permissions` — the permission slugs the plugin requests. The admin
  sees this list before approving the install.
- `source` — how the admin's server fetches the plugin on install:
  - `repo: local` — the plugin lives alongside this index under
    `<name>/`, either as a `plugin.lahx` extension package (preferred) or
    as loose `plugin.wasm` + `lahijan.manifest.yaml`. This is the default
    for the in-repo marketplace.
  - `repo: git` — clone a git URL at a pinned ref; the repo must contain
    `plugin.wasm` + `lahijan.manifest.yaml` at its root. (Future WS:
    signature verification is required for git sources in marketplace
    installs.)
- `sha256` — sha256 hex of the `.wasm` bytes the admin should expect.
  The installer verifies the downloaded bytes match before persisting.

## Adding a plugin to the default marketplace

1. Drop the plugin's extension package (named `plugin.lahx`, from
   `make package`) into a `<name>/` subdirectory of this folder, or the
   loose `plugin.wasm` + `lahijan.manifest.yaml` pair.
2. Compute the sha256 of the `.wasm`:

   ```sh
   sha256sum <name>/plugin.wasm
   ```

3. Add an entry to `plugins-marketplace.yaml` with the matching name,
   version, permissions, and sha256.

The marketplace list endpoint (`GET /api/v1/admin/marketplace`) reads
this file at bootstrap and serves it verbatim. Operators who want a
private marketplace can replace the file (or set `wasm.marketplace.url`
to a remote URL serving the same shape).

## Layout

```
examples/plugins/marketplace/
├── README.md                       # this file
├── plugins-marketplace.yaml        # the index
├── slack-notifier/
│   ├── plugin.wasm                 # (built and copied from ../slack-notifier/)
│   └── lahijan.manifest.yaml
├── autoscaler-stub/
│   ├── plugin.wasm
│   └── lahijan.manifest.yaml
└── dns-record-hook/
    ├── plugin.wasm
    └── lahijan.manifest.yaml
```
