# Self-contained install

`compose.yaml` and `.env.example` are everything a user needs to run Lahijan:

```sh
docker compose up -d
```

The user-facing guide is the website's Quickstart; the two files are shown there
verbatim (the docs build includes them from this directory, so the page can never
drift from what works).

## How it works without a checkout

The full production stack (`../docker-compose.prod.yml`) bind-mounts about ten
files from the repository. This one mounts none. The files are baked into small
images instead, built by CI from the Dockerfiles in `images/`:

| Image                     | Base                         | Adds                                                      |
| ------------------------- | ---------------------------- | --------------------------------------------------------- |
| `lahijan`                 | alpine (root `Dockerfile`)   | the server, plus the secrets shim                         |
| `lahijan-web`             | caddy                        | the built dashboard and `Caddyfile`                       |
| `lahijan-postgres`        | postgres                     | the first-boot init scripts and the PowerDNS schema       |
| `lahijan-powerdns`        | pdns-auth                    | the config template                                       |
| `lahijan-seaweedfs`       | seaweedfs                    | `filer.toml` (PostgreSQL metadata store)                  |
| `lahijan-migrate`         | migrate/migrate              | the schema migrations                                     |
| `lahijan-init`            | alpine                       | `gen-secrets` and `seed-iam`                              |

All Dockerfiles use the repository root as their build context.

## Secrets

Nothing secret is written down. The `secrets` service (`lahijan-init`) generates
the passwords and keys on first start into `/secrets/secrets.env` on a volume.
Each line is `: "${NAME:=value}"`, which only fills a variable that is still
empty, so a value set in `.env` wins. Every image that needs a secret starts
through `lahijan-env.sh`, which sources that file and then runs the original
entrypoint. The file survives restarts; deleting the volume starts from scratch
(and the old database becomes unreadable).

## One domain setting

`LAHIJAN_DOMAIN` drives everything that depends on the public address, using
compose's `${VAR:+x}${VAR:-y}` interpolation: unset gives the localhost trial
(plain HTTP on 8080, cookies not `Secure`), set gives HTTPS on the domain, S3 on
`s3.<domain>`, the matching cookie, passkey and CORS settings, and the DNS name
server. `LAHIJAN_HTTP_PORT`, `LAHIJAN_HTTPS_PORT` and `LAHIJAN_DNS_PORT` default
to non-privileged ports so a trial never collides with something already
listening; a production `.env` sets them to 80, 443 and 53.

## Build and try locally

```sh
for n in init postgres powerdns seaweedfs migrate web; do
  docker build -f deployments/install/images/$n.Dockerfile -t ghcr.io/avestura/lahijan-$n:local .
done
docker build -t ghcr.io/avestura/lahijan:local .

mkdir /tmp/trial && cp deployments/install/compose.yaml /tmp/trial/ \
  && cp deployments/install/.env.example /tmp/trial/.env \
  && echo LAHIJAN_VERSION=local >> /tmp/trial/.env
cd /tmp/trial && docker compose up -d --pull never
```

## Requirements

Docker Compose 2.20 or newer (`depends_on: required: false` for the optional
compute profile).
