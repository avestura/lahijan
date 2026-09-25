---
title: Environment file
description: Every variable in deployments/.env.prod, with its default, whether you must set it and what it controls.
---

The production stack reads its settings and secrets from `deployments/.env.prod`. You create it from `deployments/.env.prod.example` and pass it to every compose command with `--env-file`. This page lists every variable in the example file, plus the variables the compose file uses that the example leaves out.

```sh
cp deployments/.env.prod.example deployments/.env.prod
chmod 600 deployments/.env.prod
```

## How the file is used

- Docker Compose uses the file to fill in `${VAR}` references in `docker-compose.prod.yml`. A variable reaches a container only if the compose file passes it. Adding an unrelated `LAHIJAN_*` line to `.env.prod` does not change Lahijan's config.
- `scripts/backup.sh`, `scripts/restore.sh` and `scripts/upgrade.sh` load the same file with the shell (`set -a; . .env.prod`). Keep it valid shell syntax: quote values that contain spaces, and use single quotes around values that contain `$` (such as bcrypt hashes).
- Never commit `.env.prod`. It is ignored by git.

In the tables, **Default** is the value the compose file falls back to when the variable is unset or empty. "None" means the compose file has no fallback. **Required** means the stack does not work correctly without a real value.

## Public origin

| Variable                 | Default  | Required            | What it does                                                                                                                                                                                                                                                           |
| ------------------------ | -------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `LAHIJAN_PUBLIC_HOST`    | None     | Yes                 | Host name of the dashboard, for example `app.example.com`. Used as Caddy's site address; Caddy requests a certificate for it.                                                                                                                                          |
| `LAHIJAN_PUBLIC_URL`     | None     | Yes                 | Full public origin, for example `https://app.example.com`. Sets the base URL of email links (`smtp.appBaseURL`), the S3 CORS origin (`providers.seaweedfs.corsAllowedOrigins`) and Grafana's root URL. The compose file does not derive it from `LAHIJAN_PUBLIC_HOST`. |
| `LAHIJAN_ACME_EMAIL`     | Empty    | No                  | Passed to Caddy as `ACME_EMAIL`. The shipped Caddyfile does not read it; use `caddy/conf.d/acme.global` instead (see [TLS and domains](/docs/operations/tls-and-domains#acme-contact-email)).                                                                          |
| `LAHIJAN_S3_PUBLIC_HOST` | Not used | No                  | Listed in the example, but not read by the compose file or the Caddyfile.                                                                                                                                                                                              |
| `LAHIJAN_S3_PUBLIC_URL`  | Empty    | For browser uploads | Public S3 origin that pre-signed URLs are signed for. Sets `providers.seaweedfs.publicEndpoint`. Empty means URLs point at the internal `http://seaweed-s3:8333`.                                                                                                      |

## Image versions

| Variable              | Default | Required | What it does                                                                            |
| --------------------- | ------- | -------- | --------------------------------------------------------------------------------------- |
| `LAHIJAN_IMAGE_TAG`   | None    | Yes      | Tag of `ghcr.io/avestura/lahijan`. Pin a release such as `v0.1.0`; do not use `latest`. |
| `INCUS_IMAGE_TAG`     | `lts`   | No       | Tag of `ghcr.io/cmspam/incus-docker`. `lts` is Incus 6.0 LTS.                           |
| `SEAWEEDFS_IMAGE_TAG` | `3.99`  | No       | Tag of `chrislusf/seaweedfs` for all SeaweedFS services. Not in the example file.       |

## PostgreSQL

The `postgres` container creates the three databases and their roles only the first time it starts with an empty data volume. Changing these values later does not rename or re-password anything; see [Security hardening](/docs/operations/security) for rotating passwords.

| Variable                      | Default                    | Required | What it does                                                                 |
| ----------------------------- | -------------------------- | -------- | ---------------------------------------------------------------------------- |
| `POSTGRES_SUPERUSER`          | None (example: `postgres`) | Yes      | Superuser created by the image. Also used by the backup and restore scripts. |
| `POSTGRES_SUPERUSER_PASSWORD` | None                       | Yes      | Superuser password.                                                          |
| `POSTGRES_SUPERUSER_DB`       | None (example: `postgres`) | Yes      | Default database of the superuser.                                           |
| `LAHIJAN_DATABASE_NAME`       | None (example: `lahijan`)  | Yes      | Lahijan's database.                                                          |
| `LAHIJAN_DATABASE_USER`       | None (example: `lahijan`)  | Yes      | Lahijan's database role.                                                     |
| `LAHIJAN_DATABASE_PASSWORD`   | None                       | Yes      | Password of that role. Used by `lahijan`, `migrate` and the backup scripts.  |
| `PDNS_DATABASE_NAME`          | None (example: `pdns`)     | Yes      | PowerDNS database.                                                           |
| `PDNS_DATABASE_USER`          | None (example: `pdns`)     | Yes      | PowerDNS database role.                                                      |
| `PDNS_DATABASE_PASSWORD`      | None                       | Yes      | Password of that role.                                                       |
| `SEAWEED_DATABASE_NAME`       | None (example: `seaweed`)  | Yes      | SeaweedFS filer database.                                                    |
| `SEAWEED_DATABASE_USER`       | None (example: `seaweed`)  | Yes      | SeaweedFS filer database role.                                               |
| `SEAWEED_DATABASE_PASSWORD`   | None                       | Yes      | Password of that role.                                                       |

> [!WARNING]
> Keep the example database names (`lahijan`, `pdns`, `seaweed`). The second half of `deployments/postgres/init.sh` grants schema rights using those literal names.

> [!TIP]
> The `migrate` job puts `LAHIJAN_DATABASE_PASSWORD` inside a `postgres://` URL. A base64 value can contain `/`, which breaks the URL. Generate database passwords with `openssl rand -hex 32` to avoid this.

## PowerDNS

| Variable                    | Default                                                  | Required            | What it does                                                                                                                                                                   |
| --------------------------- | -------------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PDNS_API_KEY`              | None                                                     | Yes                 | Shared secret for the PowerDNS HTTP API. PowerDNS expects it, and Lahijan sends it (`LAHIJAN_PROVIDERS_POWERDNS_APIKEY`). Also used by the optional recursor.                  |
| `PDNS_WEBSERVER_ALLOW_FROM` | `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`                | No                  | Source networks allowed to reach the PowerDNS web server and API (comma-separated).                                                                                            |
| `LAHIJAN_DNS_NAMESERVERS`   | Empty                                                    | Yes, for public DNS | NS records written into every new zone, space-separated with trailing dots. Sets `providers.powerdns.defaultNameservers`. Empty keeps the config default `ns1.lahijan.local.`. |
| `PDNS_DEFAULT_SOA_CONTENT`  | `ns1.example.com. hostmaster.@ 0 10800 3600 604800 3600` | Yes, for public DNS | SOA PowerDNS writes into new zones. Keep the first field equal to your first nameserver.                                                                                       |
| `PDNS_FORWARD_ZONES`        | Empty                                                    | No                  | `dns-full` profile only. Forward zones for the PowerDNS Recursor. Not in the example file.                                                                                     |
| `PDNS_RECURSOR_ALLOW_FROM`  | `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16`                | No                  | `dns-full` profile only. Networks allowed to query the recursor. Not in the example file.                                                                                      |
| `DNSDIST_API_KEY`           | None                                                     | With `dns-full`     | `dns-full` profile only. API key for dnsdist. Not in the example file.                                                                                                         |

## SeaweedFS

| Variable                         | Default                      | Required | What it does                                                                                                                         |
| -------------------------------- | ---------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `SEAWEEDFS_S3_ACCESS_KEY`        | None                         | Yes      | Admin S3 access key. Seeded into the filer by `seaweed-iam-init` and used by Lahijan (`LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINACCESSKEY`). |
| `SEAWEEDFS_S3_SECRET_KEY`        | None                         | Yes      | Matching admin secret key (`LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINSECRETKEY`).                                                            |
| `SEAWEEDFS_S3_HOST_PORT`         | `8333`                       | No       | Host port the S3 gateway is published on.                                                                                            |
| `SEAWEEDFS_VOLUME_PUBLIC_URL`    | `http://seaweed-volume:8080` | No       | Passed to the volume server as `-publicUrl`.                                                                                         |
| `SEAWEEDFS_DEFAULT_REPLICATION`  | `000`                        | No       | Master `-defaultReplication`. `000` keeps one copy. Raise it only after adding volume servers. Not in the example file.              |
| `SEAWEEDFS_VOLUME_SIZE_LIMIT_MB` | `30000`                      | No       | Master `-volumeSizeLimitMB`. Lower it (for example `1024`) on small disks. Not in the example file.                                  |

## Incus

| Variable                                 | Default    | Required | What it does                                                                                                                                                                                          |
| ---------------------------------------- | ---------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `INCUS_KVM_GID`                          | Empty      | No       | Passed to the `incus` container as `KVM_GID`. Set it to the host `kvm` group id on hosts where `/dev/kvm` is not owned by root (for example `36` on RHEL).                                            |
| `INCUS_SOCKET_PATH`                      | Not used   | No       | Listed in the example, but the compose file uses the fixed path `/var/lib/incus`.                                                                                                                     |
| `LAHIJAN_PROVIDERS_INCUS_PLACEMENT_MODE` | Not passed | No       | Listed in the example, but the compose file does not pass it to the `lahijan` container. To use cluster placement, set it in a compose override (see [Compute cluster](/docs/admin/compute-cluster)). |

## Lahijan secrets

| Variable                             | Default | Required | What it does                                                                                                                                                                                                                                            |
| ------------------------------------ | ------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `LAHIJAN_AUTH_SIGNING_KEY`           | None    | Yes      | HMAC key for session cookies and email-link tokens (`auth.signing.key`). Lahijan refuses to start without it. Generate with `openssl rand -base64 48`. Changing it signs every user out.                                                                |
| `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` | None    | Yes      | AES-256-GCM key, base64 of 32 bytes (`auth.secrets.encryptionKey`). Encrypts TOTP secrets, external identity provider tokens and other secrets at rest. Lahijan refuses to start without it. Generate with `openssl rand -base64 32` and never lose it. |

## Email (SMTP)

| Variable                | Default | Required         | What it does                                             |
| ----------------------- | ------- | ---------------- | -------------------------------------------------------- |
| `LAHIJAN_SMTP_ENABLED`  | `true`  | No               | `smtp.enabled`. When false, emails are dropped silently. |
| `LAHIJAN_SMTP_HOST`     | None    | When enabled     | `smtp.host`.                                             |
| `LAHIJAN_SMTP_PORT`     | `587`   | No               | `smtp.port`.                                             |
| `LAHIJAN_SMTP_USER`     | None    | Depends on relay | `smtp.user`. Empty means no SMTP authentication.         |
| `LAHIJAN_SMTP_PASSWORD` | None    | Depends on relay | `smtp.password`.                                         |
| `LAHIJAN_SMTP_FROM`     | None    | When enabled     | `smtp.from`, the sender address.                         |

See [Sign-in providers and email](/docs/operations/authentication#email-and-smtp) for details.

## Passkeys (WebAuthn)

| Variable                              | Default | Required | What it does                                                                                                                               |
| ------------------------------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `LAHIJAN_AUTH_MFA_WEBAUTHN_RPID`      | Empty   | No       | `auth.mfa.webauthn.rpId`, for example `app.example.com`.                                                                                   |
| `LAHIJAN_AUTH_MFA_WEBAUTHN_RPORIGINS` | Empty   | No       | `auth.mfa.webauthn.rpOrigins`, space-separated full origins, for example `https://app.example.com`. WebAuthn is on only when both are set. |

## First-run admin

| Variable                           | Default | Required    | What it does                                                                                                                       |
| ---------------------------------- | ------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL`    | None    | Recommended | Email of the `platform.admin` user created on an empty database (`bootstrap.adminEmail`). Empty skips the bootstrap.               |
| `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD` | Empty   | No          | Preset password (`bootstrap.adminPassword`). When empty, Lahijan generates a 24-character password and logs it once at WARN level. |

## Observability

| Variable                  | Default                | Required | What it does                                                                                                                                                      |
| ------------------------- | ---------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GRAFANA_ADMIN_USER`      | `admin`                | No       | Grafana's own admin user (`GF_SECURITY_ADMIN_USER`).                                                                                                              |
| `GRAFANA_ADMIN_PASSWORD`  | None                   | Yes      | Grafana's own admin password (`GF_SECURITY_ADMIN_PASSWORD`). Grafana takes it as a plain password, even though the example file's comment mentions a bcrypt hash. |
| `GRAFANA_BASIC_AUTH_USER` | `admin`                | No       | User for Caddy's basic auth in front of `/grafana`.                                                                                                               |
| `GRAFANA_BASIC_AUTH_HASH` | bcrypt hash of `admin` | Yes      | bcrypt hash for that user. Generate with `docker run --rm caddy:2.8-alpine caddy hash-password`.                                                                  |
| `JAEGER_BASIC_AUTH_USER`  | `admin`                | No       | User for Caddy's basic auth in front of `/jaeger`.                                                                                                                |
| `JAEGER_BASIC_AUTH_HASH`  | bcrypt hash of `admin` | Yes      | bcrypt hash for that user.                                                                                                                                        |

## Backups and upgrades

The compose file does not read these. `scripts/backup.sh` and `scripts/upgrade.sh` do.

| Variable                        | Default     | What it does                                                                                             |
| ------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------- |
| `LAHIJAN_BACKUP_LOCAL_DIR`      | `./backups` | Where backup archives are written. A relative path is relative to the directory you run the script from. |
| `LAHIJAN_BACKUP_S3_BUCKET`      | Empty       | When set, each backup is also uploaded with `aws s3 cp` (needs aws-cli on the host).                     |
| `LAHIJAN_BACKUP_S3_PREFIX`      | `lahijan/`  | Key prefix for uploaded backups.                                                                         |
| `LAHIJAN_BACKUP_S3_ENDPOINT`    | Empty       | Custom S3 endpoint URL for aws-cli (`--endpoint-url`).                                                   |
| `AWS_ACCESS_KEY_ID`             | Empty       | Credentials aws-cli uses for the upload.                                                                 |
| `AWS_SECRET_ACCESS_KEY`         | Empty       | Credentials aws-cli uses for the upload.                                                                 |
| `LAHIJAN_BACKUP_RETENTION_DAYS` | `14`        | Local and uploaded backups older than this are pruned after each run.                                    |
| `LAHIJAN_GIT_BRANCH`            | `main`      | Branch `scripts/upgrade.sh` resets the checkout to. Not in the example file.                             |

## Values fixed in the compose file

These are set directly on the `lahijan` service and do not come from `.env.prod`. Change them with a compose override if you need to.

| Environment variable                                                                                                    | Value                                                         |
| ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `LAHIJAN_ENVIRONMENT`                                                                                                   | `prd`                                                         |
| `LAHIJAN_DEBUG`                                                                                                         | `false`                                                       |
| `LAHIJAN_HTTP_SERVER_PORT`, `LAHIJAN_HTTP_SERVER_HOST`                                                                  | `8080`, `0.0.0.0`                                             |
| `LAHIJAN_AUTH_SESSION_SECURE`                                                                                           | `true`                                                        |
| `LAHIJAN_AUTH_PASSWORD_MINLENGTH`                                                                                       | `12`                                                          |
| `LAHIJAN_DATABASE_HOST`, `LAHIJAN_DATABASE_PORT`, `LAHIJAN_DATABASE_SSLMODE`                                            | `postgres`, `5432`, `disable`                                 |
| `LAHIJAN_DATABASE_MAXCONNS`, `LAHIJAN_DATABASE_MINCONNS`                                                                | `30`, `5`                                                     |
| `LAHIJAN_JOBS_ENABLED`, `LAHIJAN_JOBS_ADMINUI_ENABLED`                                                                  | `true`, `true`                                                |
| `LAHIJAN_WASM_ENABLED`                                                                                                  | `true`                                                        |
| `LAHIJAN_PROVIDERS_INCUS_ENABLED`, `LAHIJAN_PROVIDERS_INCUS_SOCKETPATH`                                                 | `true`, `/var/lib/incus/unix.socket`                          |
| `LAHIJAN_PROVIDERS_POWERDNS_ENABLED`, `LAHIJAN_PROVIDERS_POWERDNS_BASEURL`                                              | `true`, `http://powerdns:8081`                                |
| `LAHIJAN_PROVIDERS_SEAWEEDFS_ENABLED`, `LAHIJAN_PROVIDERS_SEAWEEDFS_S3ENDPOINT`, `LAHIJAN_PROVIDERS_SEAWEEDFS_FILERURL` | `true`, `http://seaweed-s3:8333`, `http://seaweed-filer:8888` |
| `LAHIJAN_BOOTSTRAP_ENABLED`                                                                                             | `true`                                                        |
| `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME`                                       | `http://otel-collector:4317`, `grpc`, `lahijan`               |

Any other Lahijan setting can be added the same way. The name is `LAHIJAN_` plus the config key in upper case with dots replaced by underscores, so `auth.mfa.totp.issuer` becomes `LAHIJAN_AUTH_MFA_TOTP_ISSUER`. The full key list is in [Configuration](/docs/reference/configuration).
