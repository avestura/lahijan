---
title: Upgrades and migrations
description: Move a production stack to a new Lahijan release, apply database migrations and roll back safely.
---

An upgrade has three parts: new code in the checkout (compose file, migrations, dashboard source), a new Lahijan image, and the database migrations that go with it. This page explains how they fit together, what `scripts/upgrade.sh` does, and how to roll back.

## How migrations run

- Migrations are hand-written SQL files in `internal/app/lahijan/database/migrations`, applied with golang-migrate. Every `NNNN_name.up.sql` has a matching `NNNN_name.down.sql`.
- The Lahijan binary never migrates the database itself.
- The one-shot `migrate` service (`migrate/migrate:v4.19.1`) runs `up` against the Lahijan database. `lahijan` only starts after `migrate` exits successfully, so a plain `docker compose up -d` applies pending migrations first. When the schema is current the job does nothing.
- The `migrate` service reads the migration files from the checkout on the host (`../internal/app/lahijan/database/migrations`), not from the image. The checkout and `LAHIJAN_IMAGE_TAG` must be the same release.
- River's job tables are created by the same migrations.

## Before you upgrade

1. Read the release notes for the new version.
2. Take a backup (see [Backups](/docs/operations/backups)):

   ```sh
   scripts/backup.sh --install-dir /opt/lahijan
   ```

3. Check that the stack is healthy:

   ```sh
   docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps
   ```

## Upgrade step by step

Run these from the install directory (`/opt/lahijan`):

```sh
# 1. Get the matching code (compose file, Caddyfile, migrations, dashboard source)
git fetch origin
git checkout v0.2.0

# 2. Pin the new image
sed -i 's/^LAHIJAN_IMAGE_TAG=.*/LAHIJAN_IMAGE_TAG=v0.2.0/' deployments/.env.prod

# 3. Rebuild the dashboard that Caddy serves
make web-build

# 4. Pull images, apply migrations, restart what changed
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml pull
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml up -d
```

Step 4 runs `migrate` before recreating `lahijan`. Watch it with:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs migrate lahijan
```

## The upgrade script

`scripts/upgrade.sh [--install-dir DIR] [--tag TAG] [--force]` automates part of this. In order, it:

1. Refuses to run unless `lahijan-prod-app` is healthy (skip with `--force`).
2. Runs `git fetch` and `git reset --hard origin/<branch>`, where the branch is `LAHIJAN_GIT_BRANCH` (default `main`).
3. Pulls the `lahijan` image for the `LAHIJAN_IMAGE_TAG` in `.env.prod`.
4. Restarts `otel-collector`, recreates `lahijan` with `--no-deps`, waits up to 5 minutes for it to become healthy, then recreates `caddy`.

Know its limits before you rely on it:

- `--tag` only changes the message it prints. The image tag always comes from `LAHIJAN_IMAGE_TAG` in `.env.prod`, so edit that first.
- It recreates `lahijan` with `--no-deps`, which does not run the `migrate` service. Apply migrations yourself before or after the script:

  ```sh
  docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml run --rm migrate
  ```

- It resets the checkout to the tip of a branch, not to a release tag, and discards local edits to tracked files (for example changes to `deployments/caddy/Caddyfile`). Untracked files such as `.env.prod` and `caddy/conf.d/*` snippets are kept.
- It does not rebuild `web/dist`. Run `make web-build` afterwards.

## Check the running version

The build version is stamped into the binary at image build time (the `LAHIJAN_IMAGE_TAG` build argument; `dev` when unset). `GET /health` returns it. Caddy does not forward `/health`, so call it inside the container:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml exec lahijan \
  wget -qO- http://127.0.0.1:8080/health
```

```json
{ "status": "ok", "version": "v0.2.0" }
```

`docker inspect --format '{{.Config.Image}}' lahijan-prod-app` shows the image tag in use.

## Inspect and repair the schema version

Load the environment file so the database URL can be built, then use the `migrate` service with your own arguments:

```sh
set -a; . deployments/.env.prod; set +a
DB="postgres://${LAHIJAN_DATABASE_USER}:${LAHIJAN_DATABASE_PASSWORD}@postgres:5432/${LAHIJAN_DATABASE_NAME}?sslmode=disable"

# Current version (and whether it is dirty)
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml \
  run --rm migrate -path=/migrations -database="$DB" version
```

If a migration failed half way, golang-migrate marks the version dirty and refuses to continue. Fix the cause, check the database by hand, then mark the last good version with `force <version>` in place of `version`.

## Roll back

Rolling back the image is easy; rolling back the schema needs care, because down migrations delete the tables and columns they remove.

1. Stop Lahijan so nothing writes during the rollback:

   ```sh
   docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml stop lahijan
   ```

2. While the **new** checkout is still in place (it contains the down files for the new migrations), roll back the number of migrations the new release added. This uses the `DB` variable from the previous section. For example, for two:

   ```sh
   docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml \
     run --rm migrate -path=/migrations -database="$DB" down 2
   ```

3. Check out the old release, set the old `LAHIJAN_IMAGE_TAG`, run `make web-build` and `docker compose ... up -d`.

Older releases do not run down migrations for you. If you start an old image against a newer schema without step 2, the old binary runs against tables it does not expect.

> [!CAUTION]
> Down migrations can drop data that was written after the upgrade. When in doubt, restore the backup you took before upgrading instead.

## Other images

The other services are pinned in the compose file (for example `postgres:16-alpine`, `powerdns/pdns-auth-49:4.9.3`, `caddy:2.8-alpine`). New pins arrive with the checkout, and `docker compose pull` followed by `up -d` applies them. `incus` uses the moving `lts` tag by default (Incus 6.0 LTS); set `INCUS_IMAGE_TAG` to a fixed version if you want to control when it changes. A cold start can take longer while Incus upgrades its own database; its health check allows 60 seconds before counting failures.
