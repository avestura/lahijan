---
title: Security hardening
description: A checklist for securing a production Lahijan host, its secrets, network exposure, accounts and backups.
---

Use this checklist when you put a Lahijan stack on the internet, and go through it again after every major change. Each item says what to do and why, based on how the shipped compose stack works.

## Secrets and the environment file

- Replace every `CHANGEME` value in `deployments/.env.prod`. Generate secrets with `openssl rand`, as the example file shows. Use `openssl rand -hex 32` for database passwords (a `/` from base64 breaks the migration URL).
- Restrict the file: `chmod 600 deployments/.env.prod`, owned by root. Never commit it; it is ignored by git.
- Store a copy of `.env.prod` somewhere separate from the backup archives.
- Treat `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` as permanent. It encrypts TOTP secrets, external identity tokens and stored credentials, and there is no rotation command. If it is lost or changed, that data cannot be decrypted.
- Know that changing `LAHIJAN_AUTH_SIGNING_KEY` signs every user out and invalidates outstanding email links. Rotate it if you think it leaked, then recreate `lahijan`.
- Keep `SEAWEEDFS_S3_ACCESS_KEY` and `SEAWEEDFS_S3_SECRET_KEY` stable. The same values are used by the `seaweed-iam-init` job and by Lahijan, and Lahijan re-publishes the S3 identity document at startup. There is no tested rotation procedure for them.

Lahijan reads secrets from environment variables once at startup. The `*_FILE` variants mentioned in some comments are not implemented.

## Database passwords

Each service has its own PostgreSQL role (`lahijan`, `pdns`, `seaweed`) created on the first start. To change one later, change it in PostgreSQL first, then in `.env.prod`, then recreate the service that uses it:

```sh
NEW=$(openssl rand -hex 32)
docker exec -it lahijan-prod-postgres psql -U postgres -d postgres \
  -c "ALTER ROLE lahijan PASSWORD '$NEW';"
# set LAHIJAN_DATABASE_PASSWORD=$NEW in deployments/.env.prod, then:
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml up -d --force-recreate lahijan
```

Use `powerdns` for the `pdns` role and `seaweed-filer` for the `seaweed` role. Replace `postgres` with your `POSTGRES_SUPERUSER` if you changed it.

## The first admin account

- Sign in with the bootstrap admin and change the password immediately. The generated password is printed to the container log and stays there until the log rotates.
- Remove `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD` from `.env.prod` after the first start if you set it. The bootstrap only runs on a database without users.
- Turn on two-factor authentication for every platform admin. See [Sign-in security](/docs/account/security).

## Accounts and roles

- Registration is always open: anyone who reaches the dashboard can create an account, and by default each new account owns a personal tenant (`auth.signup.personalTenant`). Decide whether that fits your install. See [Sign-in providers and email](/docs/operations/authentication#local-passwords-and-registration).
- Review the seeded roles in [Roles and permissions](/docs/admin/roles-and-permissions). In this release `tenant.owner` receives every permission that does not start with `platform.`, which includes `compute.ip_pool.manage` and `compute.cluster.member.evacuate`, and `tenant.admin` also has `compute.cluster.member.evacuate`. Tenant owners, including self-registered users in their personal tenants, therefore pass the permission check for the operator endpoints described in [Compute cluster](/docs/admin/compute-cluster).
- If you enable SAML, keep `auth.saml.jit.enabled` off unless you want unknown IdP users to get accounts automatically, and keep `allowIdpInitiated` off unless you need it.

## TLS

- Keep TLS on in Caddy. Session cookies are `Secure` in the production file (`LAHIJAN_AUTH_SESSION_SECURE=true`), so the dashboard only works over HTTPS.
- Serve S3 over HTTPS through a `conf.d/s3.caddy` site rather than the plain HTTP port. See [TLS and domains](/docs/operations/tls-and-domains#the-s3-endpoint).

## Network exposure

The compose file publishes only these ports on the host:

| Port                             | Needed when                                                   |
| -------------------------------- | ------------------------------------------------------------- |
| 80/tcp, 443/tcp, 443/udp (Caddy) | Always                                                        |
| 53/udp, 53/tcp (PowerDNS)        | You host public DNS zones                                     |
| 8333/tcp (S3 gateway)            | Clients use the published S3 port instead of an HTTPS S3 host |

PostgreSQL, the PowerDNS API, the SeaweedFS master, volume and filer, and Lahijan itself are only on the internal `backend` network.

- Allow only the ports you need in your host or cloud firewall, and SSH from your admin addresses.
- Do not rely on a host firewall such as `ufw` to close a published Docker port. Docker's own iptables rules bypass it, and the `incus` container inserts `iptables -I DOCKER-USER -j ACCEPT` (`SETIPTABLES=true`). To close a port, stop publishing it in a compose override (for example remove `8333` when you serve S3 through Caddy) or use your provider's network firewall.
- Tighten `PDNS_WEBSERVER_ALLOW_FROM` to the Docker `backend` network instead of all private ranges. Use a long random `PDNS_API_KEY`.
- The Incus daemon runs with host networking. Lahijan only needs its Unix socket, and `deployments/incus/preseed.yaml` does not set `core.https_address`. Check with `incus config get core.https_address` inside the `incus` container, and leave it empty unless you need remote Incus access.

## Host access

- The `incus` container is privileged with host PID, network and cgroup namespaces and all of `/dev`. Anyone who can run Docker commands on the host has root on the host. Limit membership of the `docker` group.
- Keep `/var/lib/incus` readable only by root.

## Observability endpoints

- Replace the default basic-auth hashes for `/grafana` and `/jaeger` (`GRAFANA_BASIC_AUTH_HASH`, `JAEGER_BASIC_AUTH_HASH`). The fallback is `admin`/`admin`.
- Set a strong `GRAFANA_ADMIN_PASSWORD`.
- If you do not use Grafana or Jaeger from the internet, remove their routes from the Caddyfile.

## Logs and audit

- There is no log redaction handler in this release. Treat container logs as sensitive and limit who can read them.
- Review the audit log regularly. Every privileged action is recorded, and rows cannot be changed or deleted through Lahijan. See [Audit log](/docs/audit/overview).
- The River job UI at `/admin/jobs/ui` requires the `platform.jobs.read` permission. See [Background jobs](/docs/admin/jobs).

## Plugins

- Plugins run in the WASM sandbox, and an admin approves each permission a plugin asks for at install time. Read the requested permissions before approving, especially network access. See [Installing plugins](/docs/admin/plugins).
- If you do not use plugins, set `LAHIJAN_WASM_ENABLED=false` on the `lahijan` service in a compose override.

## Updates and backups

- Pin `LAHIJAN_IMAGE_TAG` to a release and upgrade deliberately. See [Upgrades and migrations](/docs/operations/upgrades).
- Pull updated images regularly: `docker compose ... pull` and `up -d`. The `incus` service follows the `lts` tag unless you pin `INCUS_IMAGE_TAG`.
- Keep the host OS and Docker patched.
- Run backups daily, copy them off the host, and test a restore. See [Backups](/docs/operations/backups), including the warning about object data.
