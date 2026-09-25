---
title: Quickstart
description: Run Lahijan on your own machine from a source checkout to evaluate it or work on it.
---

This page gets a development copy of Lahijan running on your computer: the supporting services in Docker, the Lahijan server built from source, and the dashboard in a Vite dev server. It is meant for trying Lahijan out and for contributing. To run Lahijan for real users, follow [Install on a server](/docs/getting-started/installation) instead.

> [!NOTE]
> The development setup runs DNS and object storage for real, but compute needs an Incus daemon the server can reach. Without one, the **Instances** page loads but you cannot create instances. See [Add compute (Linux only)](#add-compute-linux-only).

## What you need

- **Git**, **Docker** with the `docker compose` subcommand, and **GNU Make**.
- **Go 1.25** or newer (the version in `go.mod`).
- **Node.js 20** or newer, with npm.
- Free local ports: 5432 (Postgres), 8081 (DNS API), 5300 (DNS), 8333, 8888 and 9333 (object storage), 1025 and 8025 (mail catcher), 8080 (the Lahijan server) and 5173 (the dashboard).

On Windows, the PowerShell script described in [Windows shortcut](#windows-shortcut) does all of the steps below for you.

## 1. Get the code

```sh
git clone https://github.com/avestura/lahijan.git
cd lahijan
```

## 2. Start the supporting services

The development compose file `deployments/docker-compose.dev.yml` starts Postgres, PowerDNS, SeaweedFS and MailHog (a mail catcher for sign-up and password-reset emails). It reads its settings from `deployments/.env`.

```sh
cp deployments/.env.example deployments/.env
echo "SEAWEEDFS_VOLUME_PORT=18080" >> deployments/.env
make dev-up
```

The second line moves the object storage volume port off 8080, because the dashboard's dev server expects the Lahijan server on port 8080.

Check that the containers are up and healthy:

```sh
docker compose -f deployments/docker-compose.dev.yml ps
```

## 3. Create the database schema

The Lahijan server never changes the database schema on its own. Apply the migrations with the `migrate/migrate` container, attached to the development network:

```sh
docker run --rm --network lahijan-dev_default \
  -v "$PWD/internal/app/lahijan/database/migrations:/migrations" \
  migrate/migrate -path=/migrations \
  -database="postgres://lahijan:lahijan@postgres:5432/lahijan?sslmode=disable" up
```

If you have the `migrate` command-line tool installed, `make db-up` does the same against `localhost:5432`.

Run the same command again after you pull new code; it does nothing when the schema is already current.

## 4. Build and run the server

```sh
make build
./dist/lahijan --debug \
  --http.server.port=8080 \
  --http.server.cors.enabled \
  --database.password=lahijan \
  --smtp.enabled --smtp.host=localhost --smtp.port=1025 \
  --bootstrap.adminEmail=admin@example.com \
  --bootstrap.adminPassword='Change-me-now-2026' \
  --providers.powerdns.enabled \
  --providers.powerdns.baseURL=http://localhost:8081 \
  --providers.powerdns.apiKey=lahijan-dev-pdns-key \
  --providers.seaweedfs.enabled \
  --providers.seaweedfs.s3Endpoint=http://localhost:8333 \
  --providers.seaweedfs.filerURL=http://localhost:8888 \
  --providers.seaweedfs.adminAccessKey=lahijan_dev_admin_key \
  --providers.seaweedfs.adminSecretKey=lahijan_dev_admin_secret \
  --wasm.enabled
```

What the flags do:

- `--http.server.port=8080` matches the API proxy in the dashboard's dev server. The built-in default is 3000.
- `--database.password=lahijan` matches the database user the compose stack creates.
- `--smtp.*` sends emails to MailHog. Open `http://localhost:8025` to read them.
- `--bootstrap.*` creates the first administrator account on an empty database. The password must be at least 12 characters. If you leave out `--bootstrap.adminPassword`, the server generates one and prints it once in its log.
- `--providers.powerdns.*` and `--providers.seaweedfs.*` connect the DNS and object storage services to the containers. The keys are the development defaults from the compose stack.
- `--wasm.enabled` turns on the plugin system.

Every setting is available as a flag; see [Command line](/docs/reference/cli) and [Configuration](/docs/reference/configuration). The `environment` setting defaults to `dev`, which lets the server start without the signing and encryption keys a production deployment must set.

In a second terminal, check that the server is up:

```console
$ curl http://localhost:8080/healthcheck/liveness
OK
```

## 5. Start the dashboard

In another terminal:

```sh
make web-install
make web-dev
```

Open `http://localhost:5173` and sign in with the bootstrap email and password. The dev server forwards `/api` requests to the server on port 8080.

The Lahijan server does not serve the dashboard files itself. In production a reverse proxy serves the built dashboard; here the Vite dev server does.

## Add compute (Linux only)

The server talks to Incus over its Unix socket, `/var/lib/incus/unix.socket` by default. The simplest development setup on Linux is Incus installed on the host:

1. Install Incus following the upstream instructions, then initialize it with the repository's preseed file:

   ```sh
   incus admin init --preseed < deployments/incus/preseed.yaml
   ```

2. Make sure the user that runs `./dist/lahijan` is in the `incus-admin` group, and that `incus list` works as that user.
3. Add `--providers.incus.enabled` to the server command and restart it. If your socket is elsewhere, also pass `--providers.incus.socketPath=<path>`.

The compose file also has a containerized Incus service behind the `incus` profile. It keeps its socket inside a Docker volume, so a server running on the host cannot reach it without extra work; use the production stack if you want the containerized daemon.

## Windows shortcut

`scripts/Run-Dev.ps1` brings up the containers, applies the migrations, builds `dist\lahijan.exe`, starts it on port 8080 with flags similar to the ones above, and launches the dashboard (`http://localhost:5173`), the marketing site (`http://localhost:4173`) and the documentation site (`http://localhost:3000`).

```powershell
.\scripts\Run-Dev.ps1
```

It creates the bootstrap admin `admin@lahijan.local` with the password `Admin#Lahijan2026!` unless you pass `-AdminEmail` and `-AdminPassword`. Useful switches: `-SkipWebsite`, `-SkipDocs`, `-NoBuild` and `-Reset` (wipes the volumes and starts over). `scripts/Stop-Dev.ps1` stops the server and the frontends; add `-IncludeContainers` to stop the containers too.

Docker Desktop cannot run the containerized Incus daemon. `scripts/Setup-Incus.ps1` sets up Incus in a WSL2 distribution instead, and `Run-Dev.ps1` connects to it automatically when it finds it.

## Stop and clean up

```sh
make dev-down
```

This stops the containers and keeps their data volumes. To start from an empty database, remove the volumes too:

```sh
docker compose -f deployments/docker-compose.dev.yml down -v
```

## Next steps

- [First steps](/docs/getting-started/first-steps): a tour of the dashboard and your first instance, zone and bucket.
- [Core concepts](/docs/getting-started/concepts): tenants, roles and billing.
- [Install on a server](/docs/getting-started/installation): the production stack.
