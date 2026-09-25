---
title: Packaging (.lahx)
description: The .lahx extension package format, its size and content limits, and the lahx pack and inspect tool.
---

Lahijan installs plugins from one file: a `.lahx` extension package. This page describes the format, the checks Lahijan runs on it, and the tools that build it.

## Format

A `.lahx` file is a standard ZIP archive with exactly two files at its root:

| Entry                   | Content                                 |
| ----------------------- | --------------------------------------- |
| `lahijan.manifest.yaml` | The [manifest](/docs/plugins/manifest). |
| `plugin.wasm`           | The compiled WebAssembly module.        |

Entries may be stored or deflated. The media type is `application/vnd.lahijan.extension+zip` and the extension is `.lahx`.

Because it is a plain ZIP, any tool can build one:

```sh
zip -j my-plugin-0.1.0.lahx lahijan.manifest.yaml plugin.wasm
```

The `-j` flag stores the files without their directory, which matters: entries in subfolders are rejected.

## What Lahijan rejects

The server treats every package as untrusted. It refuses an archive when:

- it is not a valid ZIP;
- it has more than 16 entries in total;
- an entry name contains `/`, `\` or `:`, is `.` or `..`, or is not already in clean form (entries must be plain files at the root; directory entries ending in `/` are skipped);
- an entry is encrypted;
- it contains any file other than the two above, or either file twice;
- `lahijan.manifest.yaml` or `plugin.wasm` is missing;
- an entry is larger than its limit, measured both by the size declared in the archive and by the bytes actually decompressed.

### Size limits

| Limit                   | Value                                     | Where set                                      |
| ----------------------- | ----------------------------------------- | ---------------------------------------------- |
| `lahijan.manifest.yaml` | 64 KiB                                    | Fixed.                                         |
| `plugin.wasm`           | `wasm.maxModuleSize`, 10 MiB by default   | [Configuration](/docs/reference/configuration) |
| Whole upload            | module limit plus 128 KiB                 | Derived from the two above.                    |
| HTTP request body       | `http.server.bodylimit`, 4 MiB by default | [Configuration](/docs/reference/configuration) |

> [!WARNING]
> The server's general request body limit defaults to 4 MiB, which is smaller than the 10 MiB module limit. A larger package is refused before the plugin checks run. Raise `http.server.bodylimit` if you install big plugins.

After unpacking, the manifest is validated and the module is compiled. A manifest error returns `400 bad_request` with the validation messages. A module that fails to compile or declares too much memory returns `400 bad_request` with the compiler error under `details.cause`. Size errors return `413 payload_too_large`.

## The lahx tool

`cmd/lahx` in the repository builds and checks packages. It validates the manifest with the same code the server uses, so it catches mistakes before upload. Run it with Go from the repository root.

### lahx pack

```sh
go run ./cmd/lahx pack <plugin-dir> [-o out.lahx]
```

Reads `<plugin-dir>/lahijan.manifest.yaml` and `<plugin-dir>/plugin.wasm`, validates the manifest, checks that the module starts with the WebAssembly magic bytes, and writes the archive. Without `-o` the output is `<name>-<version>.lahx` inside the plugin directory. The directory defaults to the current one.

```console
$ go run ./cmd/lahx pack examples/plugins/my-plugin
wrote examples/plugins/my-plugin/my-plugin-0.1.0.lahx (my-plugin 0.1.0, 18815 bytes)
```

### lahx inspect

```sh
go run ./cmd/lahx inspect <file.lahx>
```

Opens a package with the same checks as the server (using its own caps of 64 KiB for the manifest and 64 MiB for the module), validates the manifest, and prints the name, version, module size and permissions. The server still enforces its configured limits on upload.

## make package

Each example plugin, and the template, has a `package` target:

```sh
make package
```

It builds `plugin.wasm` with TinyGo and then runs `lahx pack` on the plugin directory. It expects the Lahijan repository three levels up (`../../..`); set `LAHIJAN_ROOT` to point at another checkout:

```sh
make package LAHIJAN_ROOT=/path/to/lahijan
```

`make clean` removes `plugin.wasm` and any `.lahx` files.

## Next steps

- Upload the package: [Installing plugins](/docs/admin/plugins).
- Publish it for one-click install: [Plugin marketplace](/docs/admin/marketplace).
