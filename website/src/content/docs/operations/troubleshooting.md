---
title: Troubleshooting
description: Find and fix common problems in a production Lahijan stack, from services that will not start to failed uploads, DNS and email.
---

Start every investigation the same way: find the unhealthy service, then read its log. Most problems below show a specific message in one of the logs. Commands run from the install directory (`/opt/lahijan` by default).

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs --tail 200 lahijan
```

## A service will not start

Services start in dependency order. `lahijan` waits for `postgres`, `powerdns`, `seaweed-s3` and `incus` to be healthy and for `migrate` to finish, and `caddy` waits for `lahijan`. If `docker compose up` seems stuck, `ps` shows which dependency is not healthy yet. Read that service's log first.

### Lahijan exits at startup

Lahijan stops with a clear message when its configuration is incomplete:

| Log message                                                                                  | Cause and fix                                                                                                                  |
| -------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `failed to setup config`                                                                     | A mounted config file is not valid YAML. Fix the file.                                                                         |
| `auth.signing.key must be set in any non-dev environment`                                    | `LAHIJAN_AUTH_SIGNING_KEY` is empty. Set it in `.env.prod`.                                                                    |
| `failed to build auth deps`                                                                  | Lahijan cannot connect to PostgreSQL. See [Database connection errors](#database-connection-errors).                           |
| `auth.secrets.encryptionKey must be set in any non-dev environment` or `is not valid base64` | Set `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` to the output of `openssl rand -base64 32`.                                           |
| `oidc discovery for <name>`                                                                  | An enabled OIDC provider's issuer is wrong or unreachable. Fix `issuer` or disable the provider.                               |
| `auth.saml.spSigningKey must be set` / `auth.saml.spSigningCert must be set`                 | A SAML provider is enabled without SP credentials. See [Sign-in providers and email](/docs/operations/authentication#saml-20). |
| `providers.powerdns.apiKey must be set`                                                      | `PDNS_API_KEY` is empty.                                                                                                       |
| `providers.seaweedfs.adminAccessKey must be set`                                             | `SEAWEEDFS_S3_ACCESS_KEY` or the secret key is empty.                                                                          |
| `failed to seed rbac catalog`                                                                | The schema is missing or behind. Check the `migrate` log.                                                                      |

### The migrate job fails

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs migrate
```

- An authentication error means the Lahijan database password does not match the role in PostgreSQL (see below).
- A URL parse error usually means `LAHIJAN_DATABASE_PASSWORD` contains `/` or another character that is not valid in a URL. Change the password to a hex value.
- A `dirty` database means a migration stopped half way. See [Upgrades and migrations](/docs/operations/upgrades#inspect-and-repair-the-schema-version).

### CPU limits on small hosts

Docker refuses to create a container whose CPU limit is higher than the number of CPUs on the host. The `incus` and `lahijan` services ask for 2 CPUs. On a 1 vCPU host, lower their `deploy.resources.limits.cpus` in a compose override.

## Database connection errors

- Check that `postgres` is healthy in `docker compose ps`.
- The databases and roles are created only on the first start with an empty volume. If you changed a `*_DATABASE_PASSWORD` in `.env.prod` afterwards, PostgreSQL still has the old one. Set the new password with `ALTER ROLE` (see [Security hardening](/docs/operations/security#database-passwords)) or put the old value back.
- Keep the example database names. `deployments/postgres/init.sh` grants schema rights on the literal names `lahijan`, `pdns` and `seaweed`.

## Compute (Incus) problems

If compute requests fail, check the daemon and the socket Lahijan uses:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml ps incus
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml exec incus incus list
docker exec lahijan-prod-app ls -la /var/lib/incus/unix.socket
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs lahijan | grep -i incus
```

- `incus provider ping failed at startup; will retry lazily` means Lahijan could not reach the socket when it started. It keeps running and retries.
- The `incus` container must be privileged with host network, PID and cgroup namespaces. Do not remove those settings.
- The host needs the `vhost_vsock`, `veth` and `bridge` kernel modules; check with `ls /sys/module/vhost_vsock /sys/module/veth /sys/module/bridge`. Virtual machines also need `/dev/kvm`. On hosts where `/dev/kvm` belongs to a group, set `INCUS_KVM_GID`.
- If new instances fail because the network does not exist, apply the preseed once: it creates the `lahijanbr` bridge and the `default` storage pool (see [Deployment](/docs/operations/deployment#initialize-incus-once)).
- If instances have no internet access, check that the `incus` service still has `SETIPTABLES: "true"`, which lets bridge traffic past Docker's iptables rules.
- A "feature disabled" (`501`) answer means the provider is turned off (`providers.incus.enabled` false), not that the daemon is down.

## DNS does not resolve

Check each hop, from your server outwards:

```sh
# Does PowerDNS answer on the host?
dig @203.0.113.10 example.org SOA

# Is the zone delegated to your nameservers?
dig example.org NS +trace
```

- Port 53 must be published and open. `powerdns` publishes 53/udp and 53/tcp; if the container fails to start with an "address already in use" error, another process on the host already uses port 53 (`ss -lunp 'sport = :53'`). With the `dns-full` profile, `dnsdist` publishes port 53 too, so remove the `powerdns` mapping in an override.
- If new zones have the NS record `ns1.lahijan.local.`, `LAHIJAN_DNS_NAMESERVERS` is not set. Set it and recreate `lahijan`. Existing zones keep their old records.
- If the SOA shows `ns1.example.com.`, `PDNS_DEFAULT_SOA_CONTENT` is still the default.
- The registrar of each domain must list your nameserver names, and those names need A records (and glue records when they sit inside a delegated domain). See [TLS and domains](/docs/operations/tls-and-domains#delegating-dns-zones-to-powerdns).
- `powerdns provider ping failed at startup` in the Lahijan log means Lahijan could not reach `http://powerdns:8081`. The API key comes from `PDNS_API_KEY` for both sides, so a mismatch only happens if an override sets `LAHIJAN_PROVIDERS_POWERDNS_APIKEY` to something else.

## Browser uploads to object storage fail

Uploads from the dashboard use pre-signed URLs straight to the S3 gateway. Open the browser's developer tools and look at the failing request.

- **Request goes to `seaweed-s3:8333`.** `LAHIJAN_S3_PUBLIC_URL` is empty. Set it to your public S3 origin and recreate `lahijan`.
- **Blocked as mixed content.** The dashboard is HTTPS but the S3 URL is `http://`. Serve S3 through Caddy with a `conf.d/s3.caddy` site.
- **CORS preflight fails (403 on `OPTIONS`).** The bucket has no CORS rule for the dashboard origin. The rule comes from `LAHIJAN_PUBLIC_URL`; it is written on bucket creation and backfilled on every Lahijan start. Recreate `lahijan` and look for `seaweedfs bucket CORS backfill failed` in its log.
- **Access keys rejected.** Look for `seaweedfs IAM sync failed at startup; minted credentials may be stale` in the Lahijan log. Lahijan re-publishes the identity document at startup, so recreating `lahijan` after fixing the cause restores minted keys.

See [TLS and domains](/docs/operations/tls-and-domains#browser-uploads-and-cors) for the full setup.

## The TLS certificate is not issued

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs caddy | grep -i -E "acme|certificate|error"
```

- `LAHIJAN_PUBLIC_HOST` must be the exact host name, without `https://`.
- Its A (and AAAA, if present) record must point at this host. A stale AAAA record is a common cause.
- Ports 80 and 443 must be reachable from the internet.
- After many failed attempts Let's Encrypt rate-limits the host name. Fix the cause and wait before retrying.
- Caddy keeps certificates in the `lahijan-prod-caddy-data` volume; do not delete it on every redeploy.

## The instance console does not connect

The console is a WebSocket at `/api/v1/compute/instances/{instanceId}/console`. Caddy forwards WebSockets on `/api/*` without extra settings.

- If you run another reverse proxy in front of Caddy, it must pass the `Upgrade` and `Connection` headers and allow long-lived connections.
- The instance must be running.
- The user needs the `compute.instance.console.exec` permission.
- A daemon problem shows up in the Lahijan log and in the audit log entry `compute.instance.console.exec.connect`.

## Email is not sent

- `LAHIJAN_SMTP_ENABLED` must be `true`. When it is false, mail is dropped without an error.
- Use a port with STARTTLS, such as 587. Implicit TLS on port 465 is not supported.
- With a user name set, the server must offer STARTTLS; PLAIN authentication over an unencrypted connection is refused (except to `localhost`).
- Links in emails pointing at the wrong host mean `LAHIJAN_PUBLIC_URL` is wrong.
- A failed verification email does not block registration, so a wrong SMTP setting can go unnoticed. Test the credentials with your mail provider.

## Sign-in problems

- **Everyone was signed out.** `LAHIJAN_AUTH_SIGNING_KEY` changed.
- **Sign-in works but the session is lost immediately.** Cookies are `Secure`; the dashboard must be reached over HTTPS.
- **No first admin.** The bootstrap runs only when `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` is set and the database has no users. Find the generated password with `docker compose ... logs lahijan | grep bootstrap`.
- **Passkeys unavailable.** Set both `LAHIJAN_AUTH_MFA_WEBAUTHN_RPID` and `LAHIJAN_AUTH_MFA_WEBAUTHN_RPORIGINS`.

## Caddy shows 502 Bad Gateway

Caddy cannot reach `lahijan:8080`. Check the Lahijan container's health and log:

```sh
docker inspect --format '{{json .State.Health.Status}}' lahijan-prod-app
```
