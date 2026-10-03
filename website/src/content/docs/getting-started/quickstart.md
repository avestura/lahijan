---
title: Quickstart
description: Run Lahijan with one docker compose command. Save two files, start it, and sign in.
---

Lahijan starts with a single command. You save two files, run `docker compose up -d`, and open the dashboard. There is no source code to get, nothing to build and no database to set up.

> [!NOTE]
> You need Docker with Compose 2.20 or newer (Docker Desktop includes it) and about 4 GB of free memory.

## 1. Save the two files

Create an empty folder and save these two files in it, named exactly `compose.yaml` and `.env`:

```yaml tab="compose.yaml" include="deployments/install/compose.yaml"

```

```ini tab=".env" include="deployments/install/.env.example"

```

You do not need to read the compose file to use it. Prefer to download the files?

```sh
curl -fsSLo compose.yaml https://raw.githubusercontent.com/avestura/lahijan/main/deployments/install/compose.yaml
curl -fsSLo .env https://raw.githubusercontent.com/avestura/lahijan/main/deployments/install/.env.example
```

## 2. Start Lahijan

```sh
docker compose up -d
```

The first start downloads the images and takes a minute or two. Passwords and keys are generated for you.

## 3. Sign in

Open `http://localhost:8080` and sign in with the email address and password from your `.env` file. That is your administrator account. Change the password in **Settings** after your first sign-in.

That is all. Continue with [First steps](/docs/getting-started/first-steps) to create a DNS zone and a storage bucket.

---

Everything below is optional. Read it when you need it.

## What is running

`docker compose ps` shows the parts. They all start together, and `compose.yaml` is the only thing you have to keep:

| Part                                     | What it does                                                                                          |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `lahijan`                                | The Lahijan server: the API and the background jobs.                                                  |
| `web`                                    | The dashboard, and the front door (plain HTTP, or HTTPS once you add a domain).                       |
| `postgres`                               | The database, shared by Lahijan, DNS and object storage.                                              |
| `powerdns`                               | DNS: hosts the zones your users create.                                                               |
| `seaweed-*`                              | Object storage (S3): buckets and files.                                                               |
| `secrets`, `migrate`, `seaweed-iam-init` | One-time jobs: generate the passwords, create the tables, set up storage access. They exit when done. |

The DNS and S3 ports are published on your machine as `5353` and `8333`, so that nothing clashes with services you already run.

## Put it on your own domain

To serve Lahijan at `https://cloud.example.com` with a free, automatic certificate:

1. Point two DNS records at your server's public address: `cloud.example.com` (the dashboard) and `s3.cloud.example.com` (object storage).
2. Open ports 80 and 443 (TCP) in your firewall. To host DNS zones for your users, open port 53 (TCP and UDP) too.
3. Uncomment these lines in `.env`, using your own domain:

   ```ini title=".env"
   LAHIJAN_DOMAIN=cloud.example.com
   LAHIJAN_HTTP_PORT=80
   LAHIJAN_HTTPS_PORT=443
   LAHIJAN_DNS_PORT=53
   ```

4. Run `docker compose up -d` again.

Lahijan now answers on `https://cloud.example.com`, and the S3 endpoint is `https://s3.cloud.example.com`. The domain also sets the sign-in cookies, passkeys and the DNS name servers (`ns1.cloud.example.com`) that new zones point at. If the name servers should live on separate records, see [TLS and domains](/docs/operations/tls-and-domains). On Ubuntu, port 53 may already be taken by the system's own resolver; the [installation guide](/docs/getting-started/installation#small-hosts-and-port-53) shows how to free it.

> [!WARNING]
> Anyone who can reach your server can create an account by default. On a private installation, turn registration off before you open it to the internet: add `LAHIJAN_AUTH_SIGNUP_ENABLED=false` to `.env`, or use **Administration > Settings** after you sign in. See [Turn registration off](/docs/admin/users-and-tenants#turn-registration-off).

## Turn on compute

Containers and virtual machines need a Linux host with Incus support, so compute is off by default. On such a host, add two lines to `.env` and run `docker compose up -d`:

```ini title=".env"
COMPOSE_PROFILES=compute
LAHIJAN_COMPUTE=true
```

Docker Desktop on Windows and macOS cannot run it. See [Initialize Incus](/docs/getting-started/installation#initialize-incus) for the first-start setup, and [Compute overview](/docs/compute/overview).

## Send email

Verification and password-reset emails need a mail server. Add its details to `.env`:

```ini title=".env"
LAHIJAN_SMTP_ENABLED=true
LAHIJAN_SMTP_HOST=smtp.example.com
LAHIJAN_SMTP_PORT=587
LAHIJAN_SMTP_USER=lahijan@example.com
LAHIJAN_SMTP_PASSWORD=your-smtp-password
LAHIJAN_SMTP_FROM=Lahijan <lahijan@example.com>
```

Any other Lahijan setting can go in `.env` the same way. Each one has an environment variable name; see [Configuration](/docs/reference/configuration).

## Everyday commands

Run these in the folder with your `compose.yaml`:

```sh
docker compose logs -f lahijan   # watch the server's log
docker compose pull              # fetch newer images
docker compose up -d             # apply them (your data stays)
docker compose stop              # stop everything, keeping the data
docker compose down              # remove the containers, keeping the data
docker compose down -v           # remove everything, including all data and the generated secrets
```

By default Lahijan uses the `latest` images. To stay on one version, add `LAHIJAN_VERSION=sha-1a2b3c4` (a tag from the project's image registry) to `.env`.

> [!CAUTION]
> The generated passwords and keys live in the `lahijan_secrets` Docker volume. Back them up with the rest of your data: without them a restored database cannot be opened. `docker run --rm -v lahijan_secrets:/s alpine cat /s/secrets.env > secrets-backup.env` saves a copy. Keep it somewhere safe; it contains secrets.

## Need more control?

This install is the same Lahijan as the full production stack, with the settings chosen for you. The [installation guide](/docs/getting-started/installation) covers the longer route for operators who want to manage every file themselves, including monitoring (Grafana, Prometheus, Jaeger and Loki), recursive DNS and custom proxy settings. To change Lahijan itself, see [Run from source](/docs/getting-started/run-from-source).

## Next steps

- [First steps](/docs/getting-started/first-steps): a tour of the dashboard, and your first zone and bucket.
- [Core concepts](/docs/getting-started/concepts): tenants, roles and billing.
- [Installation guide](/docs/getting-started/installation): the detailed production guide.
