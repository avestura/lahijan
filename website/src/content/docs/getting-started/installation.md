---
title: Install on a server
description: Install the Lahijan production stack on a Linux server with Docker Compose, create the first administrator and check that everything is healthy.
---

This page walks you through a single-server installation of Lahijan with the production compose file, `deployments/docker-compose.prod.yml`. One `docker compose up` starts everything the platform needs on the host:

| Service                                                                               | What it runs                                                                                                          |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `caddy`                                                                               | Caddy 2, the public reverse proxy. Terminates TLS with automatic Let's Encrypt certificates and serves the dashboard. |
| `lahijan`                                                                             | The Lahijan server (REST API and background workers).                                                                 |
| `migrate`                                                                             | A one-shot golang-migrate job that applies database migrations before `lahijan` starts.                               |
| `postgres`                                                                            | PostgreSQL 16, shared by Lahijan, PowerDNS and the SeaweedFS filer (three databases).                                 |
| `powerdns`                                                                            | PowerDNS Authoritative, serving your users' DNS zones on port 53.                                                     |
| `seaweed-master`, `seaweed-volume`, `seaweed-filer`, `seaweed-s3`, `seaweed-iam-init` | SeaweedFS object storage and its S3 endpoint.                                                                         |
| `incus`                                                                               | The Incus daemon, in a privileged container, which runs your users' containers and virtual machines.                  |
| `otel-collector`, `jaeger`, `loki`, `prometheus`, `grafana`                           | Telemetry collection and the operator dashboards.                                                                     |

For how these fit together, see [Architecture](/docs/operations/architecture). For the full reference of the compose file, see [Deployment](/docs/operations/deployment).

> [!WARNING]
> Lahijan is not yet production-ready. Install it on a server you can rebuild, and test [backups](/docs/operations/backups) before you put real data on it.

## Before you start

### Server

- A **Linux** host with kernel 5.15 or newer. Ubuntu 22.04 or later and Debian 12 or later are the tested choices. Incus cannot run on macOS or Windows hosts.
- **4 vCPUs, 8 GB of RAM and 50 GB of disk** is a comfortable size. A 2 vCPU, 4 GB host works for a small or personal deployment.
- The kernel modules `vhost_vsock`, `veth` and `bridge`. Most distributions load them by default; check with `ls /sys/module/vhost_vsock /sys/module/veth /sys/module/bridge`.
- `/dev/kvm` if your users will create virtual machines. Containers work without it.
- A public IP address.

### Software

- **Docker Engine 24** or newer with the `docker compose` subcommand. Check with `docker version` and `docker compose version`.
- **git** and **openssl**.
- **Node.js 20** or newer and npm, to build the dashboard (you can also build it on another machine and copy it over).

You do not need to install Incus, PowerDNS, SeaweedFS or PostgreSQL on the host. They all run in containers.

### Firewall

Open these ports to the internet:

| Port | Protocol    | Used by                                                                       |
| ---- | ----------- | ----------------------------------------------------------------------------- |
| 80   | TCP         | Caddy (HTTP, redirects to HTTPS and answers Let's Encrypt challenges)         |
| 443  | TCP and UDP | Caddy (HTTPS and HTTP/3)                                                      |
| 53   | TCP and UDP | PowerDNS (authoritative DNS for your users' zones)                            |
| 8333 | TCP         | SeaweedFS S3 endpoint (object data). Change it with `SEAWEEDFS_S3_HOST_PORT`. |

Everything else stays on the internal Docker network.

### DNS records

Create these records before the first start, replacing `203.0.113.10` with your server's address:

```text
app.example.com.   A   203.0.113.10    ; the dashboard and API
ns1.example.com.   A   203.0.113.10    ; your nameservers
ns2.example.com.   A   203.0.113.10
s3.example.com.    A   203.0.113.10    ; optional: S3 endpoint with TLS
```

The dashboard record must resolve before Caddy starts, or Let's Encrypt cannot issue the certificate. The nameserver names are what your users will delegate their domains to; see [TLS and domains](/docs/operations/tls-and-domains).

## 1. Get the code

```sh
sudo git clone https://github.com/avestura/lahijan.git /opt/lahijan
cd /opt/lahijan
```

The compose file mounts configuration files, the database migrations and the built dashboard from this checkout, so keep it in place. The rest of this page assumes you work in `/opt/lahijan` as a user that can write to it and run `docker` (for example root, or give your user ownership with `sudo chown -R "$USER" /opt/lahijan`).

## 2. Create the environment file

All deployment settings and secrets live in `deployments/.env.prod`, which git ignores. Start from the example:

```sh
cp deployments/.env.prod.example deployments/.env.prod
chmod 600 deployments/.env.prod
```

Edit it and replace every `CHANGEME` value. The example file shows how to generate each secret, for instance:

```sh
openssl rand -base64 32   # database passwords, LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY
openssl rand -base64 48   # LAHIJAN_AUTH_SIGNING_KEY
openssl rand -hex 32      # PDNS_API_KEY, SEAWEEDFS_S3_SECRET_KEY
openssl rand -hex 16      # SEAWEEDFS_S3_ACCESS_KEY
```

The settings you must look at on every installation:

```ini title="deployments/.env.prod"
LAHIJAN_PUBLIC_HOST=app.example.com
LAHIJAN_PUBLIC_URL=https://app.example.com
LAHIJAN_IMAGE_TAG=v0.1.0

LAHIJAN_DNS_NAMESERVERS="ns1.example.com. ns2.example.com."
PDNS_DEFAULT_SOA_CONTENT="ns1.example.com. hostmaster.@ 0 10800 3600 604800 3600"

LAHIJAN_S3_PUBLIC_URL=http://203.0.113.10:8333

LAHIJAN_BOOTSTRAP_ADMIN_EMAIL=admin@example.com
LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD=
```

- `LAHIJAN_PUBLIC_HOST` is the host name Caddy gets a certificate for. `LAHIJAN_PUBLIC_URL` is used in email links and in the CORS settings written to every bucket.
- `LAHIJAN_DNS_NAMESERVERS` are written as `NS` records into every new zone, and `PDNS_DEFAULT_SOA_CONTENT` sets the `SOA` record. Keep the first nameserver and the SOA primary the same. If you skip these, new zones point at placeholder names.
- `LAHIJAN_S3_PUBLIC_URL` is the S3 address users reach, and the address pre-signed URLs are signed for. Use the published port as above, or `https://s3.example.com` if you set up the TLS site described in [TLS and domains](/docs/operations/tls-and-domains).
- `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` and `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD` create the first administrator. See [step 6](#6-sign-in-as-the-bootstrap-admin).

Set up outgoing email (`LAHIJAN_SMTP_*`) too: verification and password-reset emails depend on it. Change the Grafana and Jaeger basic-auth hashes from the default `admin` password.

Every variable is explained in [Environment file](/docs/operations/environment). Settings that are not in the file can be passed to the `lahijan` service as `LAHIJAN_*` environment variables; see [Configuration](/docs/reference/configuration).

> [!CAUTION]
> Back up `LAHIJAN_AUTH_SIGNING_KEY` and `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` somewhere safe. The backup script does not include them, and a restore without them cannot decrypt stored secrets. Changing the signing key signs every user out.

## 3. Build the dashboard

Caddy serves the dashboard from `web/dist` in the checkout. Build it before the first start:

```sh
make web-install
make web-build
```

If `web/dist` does not exist when the stack starts, Docker creates an empty directory and the dashboard address returns nothing. Rebuild the dashboard after every upgrade.

## 4. Get the Lahijan image

The `lahijan` service runs the image `ghcr.io/avestura/lahijan:${LAHIJAN_IMAGE_TAG}`. If that tag is published, `docker compose pull` fetches it. Otherwise build it from the checkout with the same name and tag:

```sh
docker build --build-arg LAHIJAN_IMAGE_TAG=v0.1.0 -t ghcr.io/avestura/lahijan:v0.1.0 .
```

Use the value of `LAHIJAN_IMAGE_TAG` from your environment file in both places. Pin a real version; do not use `latest`.

## 5. Start the stack

Every compose command needs the environment file and the compose file. To save typing, define an alias:

```sh
alias lahijan-compose='docker compose --env-file /opt/lahijan/deployments/.env.prod -f /opt/lahijan/deployments/docker-compose.prod.yml'
lahijan-compose up -d
```

The first start takes a few minutes: images are downloaded, Postgres creates its databases, the `migrate` job applies the schema, and Caddy requests its certificate. Services wait for the ones they depend on to become healthy.

### Initialize Incus

The Incus daemon starts empty. Initialize it once, after the first `up`, with the preseed file from the repository (it creates the default bridge, storage pool and profile):

```sh
lahijan-compose exec -T incus incus admin init --preseed < deployments/incus/preseed.yaml
lahijan-compose exec incus incus list
```

The second command should print an empty table. Incus keeps its state in `/var/lib/incus` on the host, so this survives restarts and upgrades.

### Small hosts and port 53

Two host situations need a compose override file:

- **Fewer than 2 vCPUs.** The `incus` and `lahijan` services have a CPU limit of 2.0, and Docker refuses limits above the host's CPU count.
- **Port 53 already in use.** On Ubuntu, `systemd-resolved` listens on `127.0.0.53:53`, which can stop Docker from publishing port 53 on all addresses. Publish it on the public address only.

```yaml title="deployments/docker-compose.override.yml"
services:
  incus:
    deploy:
      resources:
        limits:
          cpus: "1.0"
  lahijan:
    deploy:
      resources:
        limits:
          cpus: "1.0"
  powerdns:
    ports: !override
      - "203.0.113.10:53:53/udp"
      - "203.0.113.10:53:53/tcp"
```

Because the commands name the compose file with `-f`, Docker does not pick up the override automatically. Add it to the alias: `-f /opt/lahijan/deployments/docker-compose.override.yml`. The `!override` tag needs Docker Compose 2.24 or newer.

## 6. Sign in as the bootstrap admin

On its first start against an empty database, the Lahijan server creates a default tenant and a user with the **platform administrator** role. This is controlled by the `bootstrap.*` settings:

| Setting                             | Variable in `.env.prod`             | Default                  | Meaning                                                |
| ----------------------------------- | ----------------------------------- | ------------------------ | ------------------------------------------------------ |
| `bootstrap.enabled`                 | (set to `true` by the compose file) | `true`                   | Turns the first-run bootstrap on or off.               |
| `bootstrap.adminEmail`              | `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL`     | empty                    | Email of the admin account. Empty skips the bootstrap. |
| `bootstrap.adminPassword`           | `LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD`  | empty                    | The admin's password. Empty generates a random one.    |
| `bootstrap.adminDisplayName`        | none                                | `Platform Administrator` | Display name of the admin account.                     |
| `bootstrap.generatedPasswordLength` | none                                | `24`                     | Length of the generated password.                      |

The compose file passes the two `.env.prod` variables to the server as `LAHIJAN_BOOTSTRAP_ADMINEMAIL` and `LAHIJAN_BOOTSTRAP_ADMINPASSWORD`.

If you left the password empty, read the generated one from the server log. It is printed once:

```sh
lahijan-compose logs lahijan | grep "first-run admin credentials"
```

The bootstrap runs only while the `users` table is empty. Once any account exists it is skipped on every start, so changing these variables later has no effect.

Open `https://app.example.com`, sign in with the admin email and password, and change the password right away under **Settings > Profile > Change password**. Then continue with [First steps](/docs/getting-started/first-steps).

> [!TIP] > `scripts/install.sh` automates steps 1, 2 and 5: it checks the prerequisites, clones the repository to `/opt/lahijan`, asks for the main values and writes `.env.prod`, pulls the images, starts the stack and prints the bootstrap credentials. It does not build the dashboard or the Lahijan image, and it does not initialize Incus, so do steps 3, 4 and the Incus initialization yourself.

## 7. Check that it is healthy

List the services and their health:

```sh
lahijan-compose ps
```

Every long-running service should show `(healthy)`. The one-shot `migrate` and `seaweed-iam-init` jobs are not listed because they have finished; `lahijan-compose ps -a` shows them with exit code 0.

Check the server through Caddy:

```console
$ curl https://app.example.com/healthcheck/liveness
OK
```

Then check each service from the outside:

- **DNS:** after you create a zone in the dashboard, `dig @203.0.113.10 example.org SOA` should return the SOA record.
- **Object storage:** `curl -I http://203.0.113.10:8333/status` should answer.
- **Operator dashboards:** Grafana is at `https://app.example.com/grafana/` and Jaeger at `https://app.example.com/jaeger/`, both behind basic auth.

If a service does not become healthy, look at its log with `lahijan-compose logs <service>` and see [Troubleshooting](/docs/operations/troubleshooting). Common first-install problems:

- The dashboard shows a certificate warning: the `A` record for `LAHIJAN_PUBLIC_HOST` does not point at the server yet, or port 80 is blocked. Caddy's log shows the ACME error.
- The `incus` service never becomes healthy: a kernel module is missing, or the image tag in `INCUS_IMAGE_TAG` does not start on your host. Check `lahijan-compose logs incus`.
- The **Instances** page says the feature is disabled: the server cannot reach the Incus socket. Check that `incus` is healthy and that `/var/lib/incus/unix.socket` exists on the host.

## Next steps

- [First steps](/docs/getting-started/first-steps): create your first resources and add users.
- [TLS and domains](/docs/operations/tls-and-domains): an S3 host name with TLS, the ACME contact email and nameserver delegation.
- [Sign-in providers and email](/docs/operations/authentication): SMTP, OAuth, OIDC, SAML and passkeys.
- [Backups](/docs/operations/backups) and [Upgrades and migrations](/docs/operations/upgrades).
- [Security hardening](/docs/operations/security).
