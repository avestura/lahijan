---
title: Command line
description: The lahijan server binary, its generated flags, environment variables and config file lookup, and the lahx packaging tool.
---

The repository builds two programs, both under `cmd/`:

| Binary    | Source        | What it does                                                                        |
| --------- | ------------- | ----------------------------------------------------------------------------------- |
| `lahijan` | `cmd/lahijan` | The Lahijan server: REST API, background workers and plugin runtime in one process. |
| `lahx`    | `cmd/lahx`    | Builds and checks `.lahx` plugin packages.                                          |

## lahijan

### Build and run

```sh
make build          # writes dist/lahijan
./dist/lahijan      # starts the server with the built-in defaults
```

`make build` stamps the version into the binary (`VERSION=v0.2.0 make build`; the default is `dev`). `make run` builds and starts the server with `--debug --http.server.cors.enabled`.

In the container image built from the repository's `Dockerfile`, the binary is `/etc/lahijan/server` and the working directory is `/etc/lahijan`.

### What happens at startup

`lahijan` has no subcommands. Running it starts the server, which:

1. Loads the configuration (see [Configuration sources](#configuration-sources)).
2. Refuses to start if `environment` is not a development value (`dev`, `development`, `local` or empty) and `auth.signing.key` is empty.
3. Connects to PostgreSQL and builds each subsystem that is turned on: sign-in, background jobs, the plugin runtime, and the compute, DNS and object storage providers. A subsystem that is turned off answers its API routes with a "feature disabled" error instead.
4. Seeds the built-in permissions and roles, and runs the [first-run admin bootstrap](/docs/getting-started/installation#6-sign-in-as-the-bootstrap-admin) on an empty database.
5. Listens on `http.server.host`:`http.server.port` (default `0.0.0.0:3000`).

The server does **not** apply database migrations. Run them with golang-migrate before you start a new version: the production compose stack has a `migrate` service for this, and `make db-up` does it in development. See [Upgrades and migrations](/docs/operations/upgrades).

The server does not serve the dashboard's files either. A reverse proxy (Caddy in the production stack) or the Vite dev server does that.

Once it is listening, these endpoints need no authentication:

| Endpoint                     | Returns                                     |
| ---------------------------- | ------------------------------------------- |
| `GET /healthcheck/liveness`  | `200 OK` while the process is up.           |
| `GET /healthcheck/readiness` | `200 OK` while the process is up.           |
| `GET /health`                | JSON with `status` and the build `version`. |
| `GET /api/v1/ping`           | JSON with the server's current time.        |

The two `/healthcheck/*` paths are set by `http.server.healthcheck.livenessEndpoint` and `readinessEndpoint`, and the whole pair can be turned off with `http.server.healthcheck.enabled`.

### Configuration sources

Every setting has a dotted name taken from the built-in defaults file, `internal/app/lahijan/conf/.lahijan.conf.default.yaml`, which is compiled into the binary. You can set it in four ways. Each one overrides the ones below it:

1. **A command-line flag**, `--<dotted.name>`.
2. **An environment variable**, `LAHIJAN_` plus the name in capitals with dots replaced by underscores.
3. **A config file** named `.lahijan.conf.default.yaml`. The server looks in `/etc/lahijan`, then `$HOME/.lahijan`, then the working directory, and merges the first file it finds over the built-in defaults. The file only needs the settings you change.
4. **The built-in default.**

| Setting                      | Flag                                                | Environment variable                                      |
| ---------------------------- | --------------------------------------------------- | --------------------------------------------------------- |
| `http.server.port`           | `--http.server.port=8080`                           | `LAHIJAN_HTTP_SERVER_PORT=8080`                           |
| `database.maxConns`          | `--database.maxConns=30`                            | `LAHIJAN_DATABASE_MAXCONNS=30`                            |
| `providers.powerdns.baseURL` | `--providers.powerdns.baseURL=http://powerdns:8081` | `LAHIJAN_PROVIDERS_POWERDNS_BASEURL=http://powerdns:8081` |

Flag names keep the exact case of the setting (`--database.maxConns`). Environment variable names do not care about case, and a camel-case part is simply written in capitals without an extra underscore: `maxConns` becomes `MAXCONNS`, not `MAX_CONNS`.

The full list of settings, with their defaults and environment variable names, is in [Configuration](/docs/reference/configuration).

> [!NOTE]
> The variables in the production `deployments/.env.prod` file are read by Docker Compose, not by the server, and some have different names. For example, `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` in `.env.prod` reaches the server as `LAHIJAN_BOOTSTRAP_ADMINEMAIL`. See [Environment file](/docs/operations/environment).

### Flags

The flags are generated at startup from the defaults file, one per setting, so every setting in [Configuration](/docs/reference/configuration) is also a flag. The flag's type follows the default value:

| Default value         | Flag type                            | Example                                                                       |
| --------------------- | ------------------------------------ | ----------------------------------------------------------------------------- |
| `true` / `false`      | Boolean. The bare flag means `true`. | `--debug`, `--smtp.enabled`, `--providers.incus.events.enabled=false`         |
| A whole number        | Integer                              | `--http.server.port=8080`                                                     |
| A decimal number      | Float                                |                                                                               |
| A list                | Comma-separated values               | `--http.server.cors.allowOrigins=https://a.example.com,https://b.example.com` |
| An IP address or CIDR | IP or network                        |                                                                               |
| Anything else         | String                               | `--environment=prd`                                                           |

`--help` prints every flag with its default and the description from the defaults file. An unknown flag or a value of the wrong type prints an error and the flag list, and the server exits with status 2.

A few flags you will use often:

| Flag                                                                                         | Default            | Purpose                                                                          |
| -------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------- |
| `--debug`                                                                                    | `false`            | More verbose logging.                                                            |
| `--environment`                                                                              | `dev`              | `dev` allows development fallbacks for missing secrets. Use `prd` in production. |
| `--http.server.port`                                                                         | `3000`             | Port to listen on.                                                               |
| `--database.host`, `--database.password`                                                     | `localhost`, empty | PostgreSQL connection.                                                           |
| `--auth.signing.key`                                                                         | empty              | Key that signs session cookies. Required outside development.                    |
| `--providers.incus.enabled`, `--providers.powerdns.enabled`, `--providers.seaweedfs.enabled` | `false`            | Turn on compute, DNS and object storage.                                         |
| `--jobs.enabled`                                                                             | `false`            | Turn on the background job queue.                                                |
| `--wasm.enabled`                                                                             | `false`            | Turn on the plugin system.                                                       |

> [!WARNING]
> Flags are visible to other users of the machine in the process list. Pass secrets such as `database.password` and `auth.signing.key` as environment variables or in a config file only the server can read.

## lahx

`lahx` builds `.lahx` plugin packages and checks existing ones. A `.lahx` file is a ZIP archive holding two entries: the manifest `lahijan.manifest.yaml` and the compiled module `plugin.wasm`. `lahx` validates the manifest with the same rules the server applies at upload time, so you catch mistakes before you upload. See [Packaging (.lahx)](/docs/plugins/packaging).

Run it from the repository, or build it once:

```sh
go run ./cmd/lahx <command> ...
go build -o dist/lahx ./cmd/lahx
```

### lahx pack

```sh
lahx pack <plugin-dir> [-o <file.lahx>]
```

Reads `<plugin-dir>/lahijan.manifest.yaml` and `<plugin-dir>/plugin.wasm`, validates the manifest, checks that `plugin.wasm` is a WebAssembly module, and writes the package.

| Argument or flag | Default                              | Meaning                                                                    |
| ---------------- | ------------------------------------ | -------------------------------------------------------------------------- |
| `<plugin-dir>`   | `.`                                  | Directory holding the manifest and module. It can go before or after `-o`. |
| `-o <file>`      | `<plugin-dir>/<name>-<version>.lahx` | Output path. The name and version come from the manifest.                  |

```console
$ lahx pack examples/plugins/slack-notifier
wrote examples/plugins/slack-notifier/slack-notifier-1.0.0.lahx (slack-notifier 1.0.0, 1234567 bytes)
```

Build the module first; `pack` stops with an error if `plugin.wasm` is missing.

### lahx inspect

```sh
lahx inspect <file.lahx>
```

Opens a package, checks that both entries are present and that the manifest is valid, and prints the plugin's name, version, module size and requested permissions.

```console
$ lahx inspect slack-notifier-1.0.0.lahx
slack-notifier 1.0.0
  module: 1230000 bytes
  permissions: [events.listen:compute.instance.* network.outbound:hooks.slack.com config.read:slack-notifier]
```

`inspect` accepts manifests up to 64 KiB and modules up to 64 MiB. The server applies its own, usually smaller, limit on upload (`wasm.maxModuleSize`, 10 MiB by default).

On any error, `lahx` prints `lahx: <error>` and exits with status 1.
