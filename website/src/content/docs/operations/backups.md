---
title: Backups
description: What to back up in a Lahijan install, how to do it with the shipped scripts and plain Docker commands, and the order to restore in.
---

A Lahijan install keeps state in PostgreSQL, the SeaweedFS data volume, the Incus directory on the host and the environment file. This page lists each one, shows how to copy it, and gives the order to restore in. Test a full restore on a spare machine before you depend on it.

All commands run from the install directory (`/opt/lahijan` by default).

## What to back up

| Item                                                                        | Location                                                       | Covered by `scripts/backup.sh`  |
| --------------------------------------------------------------------------- | -------------------------------------------------------------- | ------------------------------- |
| Lahijan database (users, tenants, audit log, billing, jobs, plugin modules) | `lahijan` database in container `lahijan-prod-postgres`        | Yes                             |
| PowerDNS zones and records                                                  | `pdns` database                                                | Yes                             |
| SeaweedFS metadata and S3 identities                                        | `seaweed` database                                             | Yes                             |
| PostgreSQL roles                                                            | cluster globals                                                | Yes (`pg_dumpall --roles-only`) |
| Object data                                                                 | Docker volume `lahijan-prod-seaweedfs`                         | No, see the warning below       |
| Plugin volume                                                               | Docker volume `lahijan-prod-plugins`                           | Yes                             |
| Incus instances, images, storage pools and database                         | host directory `/var/lib/incus`                                | Yes, when run as root           |
| Secrets and settings                                                        | `deployments/.env.prod`                                        | No                              |
| Caddy snippets                                                              | `deployments/caddy/conf.d/`                                    | No                              |
| Config file, if you mount one                                               | for example `deployments/lahijan.yaml`                         | No                              |
| TLS certificates                                                            | volumes `lahijan-prod-caddy-data`, `lahijan-prod-caddy-config` | No (Caddy issues new ones)      |
| Metrics and dashboards                                                      | volumes `lahijan-prod-prometheus`, `lahijan-prod-grafana`      | No                              |

> [!CAUTION]
> Without `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` you cannot read encrypted columns (TOTP secrets, external identity tokens, stored credentials) in a restored database. Without `LAHIJAN_AUTH_SIGNING_KEY` every session and email link becomes invalid. Keep a copy of `.env.prod` in a separate, secure place from the backup archives.

## The backup script

```sh
scripts/backup.sh --install-dir /opt/lahijan
```

Options: `--install-dir DIR` (default `/opt/lahijan`) and `--out FILE` to choose the archive path. The script:

1. Dumps the three databases with `pg_dump -Fc --no-owner --no-privileges` as `POSTGRES_SUPERUSER`, plus the roles.
2. Archives a SeaweedFS volume and `lahijan-prod-plugins` with a temporary `alpine:3.22` container.
3. Archives `/var/lib/incus` from the host. It skips this if the directory does not exist and continues with a warning if `tar` fails.
4. Writes a `MANIFEST` with the image tags and database names.
5. Bundles everything as `lahijan-backup-<UTC stamp>.tar.gz` in `LAHIJAN_BACKUP_LOCAL_DIR` (default `./backups`).
6. Uploads the archive with `aws s3 cp` when `LAHIJAN_BACKUP_S3_BUCKET` is set, then prunes local and uploaded archives older than `LAHIJAN_BACKUP_RETENTION_DAYS` (default 14).

Run it daily from root's crontab:

```text
0 2 * * * /opt/lahijan/scripts/backup.sh --install-dir /opt/lahijan >> /var/log/lahijan-backup.log 2>&1
```

> [!WARNING]
> The script reads object data from a volume named `lahijan-prod-seaweed`, but the compose file names it `lahijan-prod-seaweedfs`. Docker creates an empty volume under the wrong name, so the `seaweedfs.tgz` in the archive is empty and your objects are not backed up. Until the script is fixed, back up the object data volume yourself as shown below.

The script copies SeaweedFS and Incus data while the services run. For a consistent copy of those, stop the services first (next section).

## Manual commands

### PostgreSQL

```sh
set -a; . deployments/.env.prod; set +a
mkdir -p backups
for db in "$LAHIJAN_DATABASE_NAME" "$PDNS_DATABASE_NAME" "$SEAWEED_DATABASE_NAME"; do
  docker exec lahijan-prod-postgres pg_dump -U "$POSTGRES_SUPERUSER" -d "$db" -Fc > "backups/$db.dump"
done
docker exec lahijan-prod-postgres pg_dumpall -U "$POSTGRES_SUPERUSER" --roles-only > backups/roles.sql
```

`pg_dump` gives a consistent snapshot while the stack runs.

### SeaweedFS object data

Stop the SeaweedFS services so no volume file changes during the copy, archive the volume, then start them again:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml \
  stop seaweed-s3 seaweed-filer seaweed-volume seaweed-master

docker run --rm -v lahijan-prod-seaweedfs:/data:ro -v "$PWD/backups":/out alpine:3.22 \
  tar -C /data -czf /out/seaweedfs.tgz .

docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml \
  start seaweed-master seaweed-volume seaweed-filer seaweed-s3
```

The file metadata that goes with this data is in the `seaweed` database. Back up both at the same time.

### Incus

Incus state is in `/var/lib/incus` on the host. For a consistent copy, stop the `incus` service (plan for instance downtime), archive the directory as root and start it again:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml stop incus
sudo tar -C /var/lib/incus -czf backups/incus.tgz .
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml start incus
```

With a large `dir` storage pool this archive can be big. Users can also back up their own instances from the dashboard; see [Snapshots and backups](/docs/compute/snapshots-and-backups).

### Configuration and certificates

```sh
cp deployments/.env.prod /secure/location/lahijan.env.prod
tar -czf backups/caddy-conf.d.tgz deployments/caddy/conf.d
docker run --rm -v lahijan-prod-caddy-data:/data:ro -v "$PWD/backups":/out alpine:3.22 \
  tar -C /data -czf /out/caddy-data.tgz .
```

Caddy data is optional: without it Caddy requests new certificates, which counts against Let's Encrypt rate limits if you restore often.

## Restore

Restore in this order:

1. **Host and code.** Prepare the host as for a new install, clone the repository and check out the same release as the backup (`lahijan_image_tag` in `MANIFEST`).
2. **Secrets.** Put back the original `deployments/.env.prod`. The signing and encryption keys must be the ones used when the backup was taken.
3. **Databases.** Restore `lahijan`, `pdns` and `seaweed`.
4. **Object data.** Restore the `lahijan-prod-seaweedfs` volume.
5. **Plugins and Incus.** Restore `lahijan-prod-plugins` and `/var/lib/incus`.
6. **Start.** `docker compose ... up -d`. The `migrate` job then brings the schema up to the checked-out release if needed.

`scripts/restore.sh` handles steps 3 to 5 from an archive made by `backup.sh`, with one gap for object data described below:

```sh
sudo scripts/restore.sh --install-dir /opt/lahijan --backup backups/lahijan-backup-20260101T020000Z.tar.gz
```

It stops every service except PostgreSQL, drops and recreates the three databases, restores the roles and each dump, replaces the plugin volume and, when run as root, replaces `/var/lib/incus`. It asks you to type the value of `LAHIJAN_PUBLIC_HOST` to confirm; `--yes` skips the prompt. When it finishes, it prints the command to start the stack again. The first-run admin bootstrap is skipped because users already exist, so sign in with the restored accounts.

> [!WARNING]
> Like the backup script, `restore.sh` writes object data into `lahijan-prod-seaweed` instead of `lahijan-prod-seaweedfs`. Restore the object volume yourself before starting the stack:

```sh
docker run --rm -v lahijan-prod-seaweedfs:/data alpine:3.22 sh -c 'rm -rf /data/* /data/.[!.]* 2>/dev/null; true'
docker run --rm -v lahijan-prod-seaweedfs:/data -v "$PWD/backups":/out:ro alpine:3.22 \
  tar -C /data -xzf /out/seaweedfs.tgz
```

After the stack is up, check that it is healthy and that DNS, buckets and instances are all there:

```sh
docker inspect --format '{{json .State.Health.Status}}' lahijan-prod-app
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml exec incus incus list --all-projects
```
