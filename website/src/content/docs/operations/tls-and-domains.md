---
title: TLS and domains
description: Choose host names, get certificates from Caddy, publish an HTTPS S3 endpoint, delegate DNS zones to PowerDNS and allow browser uploads.
---

A public Lahijan install uses up to three kinds of host names: the dashboard host, an optional S3 host and the nameservers that PowerDNS answers for. This page shows the DNS records to create, how Caddy gets certificates and which settings tie the names together. The examples use `example.com`; replace it with your domain.

## Host names

| Name               | Example                              | Used for                                       | Setting                                               |
| ------------------ | ------------------------------------ | ---------------------------------------------- | ----------------------------------------------------- |
| Dashboard host     | `app.example.com`                    | Dashboard, REST API, River UI, Grafana, Jaeger | `LAHIJAN_PUBLIC_HOST`, `LAHIJAN_PUBLIC_URL`           |
| S3 host (optional) | `s3.example.com`                     | S3 data plane over HTTPS                       | `conf.d/s3.caddy`, `LAHIJAN_S3_PUBLIC_URL`            |
| Nameservers        | `ns1.example.com`, `ns2.example.com` | Authoritative DNS for your users' zones        | `LAHIJAN_DNS_NAMESERVERS`, `PDNS_DEFAULT_SOA_CONTENT` |

## DNS records to create

Create these at your DNS provider before the first `docker compose up`, with `203.0.113.10` standing for the host's public IP:

```text
app.example.com.   A   203.0.113.10
s3.example.com.    A   203.0.113.10    ; only if you use an S3 host
ns1.example.com.   A   203.0.113.10    ; only if the host serves DNS
ns2.example.com.   A   203.0.113.10
```

Add `AAAA` records too if the host has IPv6. On a single host both nameserver names point at the same address, because only one PowerDNS server runs.

## Automatic TLS with Caddy

Caddy reads `deployments/caddy/Caddyfile`. Its main site address is `{$LAHIJAN_PUBLIC_HOST}`, so setting `LAHIJAN_PUBLIC_HOST=app.example.com` is enough for Caddy to request a Let's Encrypt certificate on first boot and redirect HTTP to HTTPS. Certificates and the ACME account are kept in the `lahijan-prod-caddy-data` volume.

Certificate issuance needs:

- The A record for the dashboard host already pointing at this host.
- Ports 80 and 443 reachable from the internet.

Watch issuance in the Caddy log:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml logs -f caddy
```

### Extra Caddy configuration

The directory `deployments/caddy/conf.d/` is mounted at `/etc/caddy/conf.d`. The Caddyfile imports two kinds of files from it:

- `*.global` files go into the global options block.
- `*.caddy` files are added as extra site blocks.

Both are optional, and files you create there are ignored by git.

### ACME contact email

The compose file passes `LAHIJAN_ACME_EMAIL` to the Caddy container as `ACME_EMAIL`, but the shipped Caddyfile does not read that variable. To register a contact email with Let's Encrypt, add a global snippet:

```sh
echo "email ops@example.com" > deployments/caddy/conf.d/acme.global
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml restart caddy
```

## The S3 endpoint

Users' S3 clients and browsers send object data straight to the SeaweedFS S3 gateway. You have two ways to expose it.

**Published port (default).** `seaweed-s3` publishes `SEAWEEDFS_S3_HOST_PORT` (default 8333) over plain HTTP. Set `LAHIJAN_S3_PUBLIC_URL=http://203.0.113.10:8333` or a host name that resolves to the host.

**HTTPS host through Caddy (recommended).** Create a site snippet and point the public URL at it:

```text title="deployments/caddy/conf.d/s3.caddy"
s3.example.com {
	reverse_proxy seaweed-s3:8333
}
```

```ini title="deployments/.env.prod"
LAHIJAN_S3_PUBLIC_URL=https://s3.example.com
```

Then recreate Caddy and Lahijan:

```sh
docker compose --env-file deployments/.env.prod -f deployments/docker-compose.prod.yml up -d --force-recreate caddy lahijan
```

`LAHIJAN_S3_PUBLIC_URL` sets `providers.seaweedfs.publicEndpoint`, the origin that pre-signed URLs are signed for. When it is empty, pre-signed URLs point at the internal address `http://seaweed-s3:8333`, which no user can reach.

> [!TIP]
> Browsers block requests from an HTTPS page to a plain HTTP address. If users upload or download objects from the dashboard, use the HTTPS S3 host.

`LAHIJAN_S3_PUBLIC_HOST` appears in the example environment file, but neither the compose file nor the Caddyfile reads it. The `s3.caddy` snippet is what creates the S3 site.

## Browser uploads and CORS

The dashboard uploads and downloads objects with pre-signed URLs straight from the browser. For that, every bucket needs a CORS rule that allows the dashboard origin. Lahijan writes this rule from `providers.seaweedfs.corsAllowedOrigins`:

- when a bucket is created, and
- for every existing bucket each time Lahijan starts and can reach SeaweedFS.

The production compose file sets it to `LAHIJAN_PUBLIC_URL` (`LAHIJAN_PROVIDERS_SEAWEEDFS_CORSALLOWEDORIGINS`). The rule allows `GET`, `PUT`, `HEAD`, `POST` and `DELETE`, all request headers, and exposes `ETag` and `x-amz-version-id`. When the list is empty, no rule is written and browser uploads fail the CORS preflight.

Because the compose file sets this variable, a value in a config file does not win over it (environment variables take precedence). To allow more than one origin, set a space-separated list in a compose override file:

```yaml title="deployments/docker-compose.override.yml"
services:
  lahijan:
    environment:
      LAHIJAN_PROVIDERS_SEAWEEDFS_CORSALLOWEDORIGINS: "https://app.example.com https://console.example.com"
```

After changing the origins, recreate the `lahijan` container so it backfills the rule onto existing buckets.

## Delegating DNS zones to PowerDNS

PowerDNS publishes port 53 (UDP and TCP) on the host. For zones your users create to resolve on the internet:

1. Choose the nameserver names and create their A records (above). If the nameservers are inside a domain you delegate to this host, also create glue records for them at your registrar.
2. Set the NS records Lahijan writes into every new zone:

   ```ini title="deployments/.env.prod"
   LAHIJAN_DNS_NAMESERVERS="ns1.example.com. ns2.example.com."
   PDNS_DEFAULT_SOA_CONTENT="ns1.example.com. hostmaster.@ 0 10800 3600 604800 3600"
   ```

   `LAHIJAN_DNS_NAMESERVERS` sets `providers.powerdns.defaultNameservers` (space-separated, trailing dots). If you leave it empty, the config default `ns1.lahijan.local.` is used, which does not resolve. `PDNS_DEFAULT_SOA_CONTENT` is the SOA PowerDNS writes into new zones; keep its first field equal to your first nameserver.

3. Open UDP and TCP port 53 in the host firewall.
4. For each user zone, the domain owner sets the NS records at their registrar to your nameserver names.
5. Check from outside:

   ```sh
   dig @203.0.113.10 example.org SOA
   dig example.org NS
   ```

These settings only apply to zones created after the change. Existing zones keep the NS and SOA records they were created with.

AXFR to secondary servers is off by default (`providers.powerdns.defaultAXFREnabled`). See [DNS zones](/docs/dns/zones) for the user side.
