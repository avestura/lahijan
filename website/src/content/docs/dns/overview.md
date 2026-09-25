---
title: DNS overview
description: How the Lahijan DNS service hosts authoritative zones for your domains, how delegation works and which permissions you need.
---

The DNS service lets you host the authoritative DNS for domains you own. You create a zone for a domain, add records to it, and optionally sign it with DNSSEC. Lahijan's nameservers then answer queries for that domain on the public internet.

You can manage DNS from the dashboard under **DNS**, or through the REST API under `/api/v1/dns/`.

## What the service does

The DNS service is **authoritative only**. It serves the zones you create and nothing else. It is not a recursive resolver, so you do not point your computers or servers at it to look up other names.

Everything is organized around three objects:

| Object   | What it is                                                         | Page                                  |
| -------- | ------------------------------------------------------------------ | ------------------------------------- |
| Zone     | One domain (for example `example.com.`) and every name under it.   | [Zones](/docs/dns/zones)              |
| Record   | One entry in a zone, such as an `A` record for `www.example.com.`. | [Records](/docs/dns/records)          |
| Template | A predefined set of records you can add to a zone in one step.     | [Zone templates](/docs/dns/templates) |

You can also turn on [DNSSEC](/docs/dns/dnssec) per zone. If your operator has enabled domain registration, you can search, register, renew and transfer domain names through the [Domains](/docs/dns/domains) API.

Zones and records belong to the tenant you are working in. Other tenants cannot see them. A zone name is unique across the whole platform, so two tenants cannot host the same domain.

## How delegation works

Creating a zone in Lahijan does not make it live on the internet by itself. Resolvers find your zone through **delegation**: the registry for your top-level domain (for example `.com`) must list Lahijan's nameservers as the nameservers for your domain.

The steps are:

1. Create the zone in Lahijan (see [Zones](/docs/dns/zones)).
2. Add the records you need.
3. At the registrar where you bought the domain, set the domain's nameservers to the nameservers your operator configured for this Lahijan installation.
4. Wait for the change to spread. Registries and resolvers cache delegation data, so this can take from minutes to a day or more.

When Lahijan creates a zone, it writes the operator's nameservers into the zone's `NS` records and its `SOA` record automatically. You do not need to add them yourself.

> [!NOTE]
> The dashboard does not show the nameserver names. Ask your operator which nameservers to use at your registrar, or look them up once the zone exists:
>
> ```sh
> dig NS example.com @<one-of-your-operator-nameservers>
> ```

If you register a domain through Lahijan's [Domains](/docs/dns/domains) API, the registration happens through your operator's registrar account, but you still need to confirm the domain is delegated to the right nameservers.

## Permissions

Every DNS action is checked against your role in the tenant. The dashboard hides or disables buttons you are not allowed to use and shows "You do not have permission to perform this action." where relevant.

| Permission            | Allows                                                                       |
| --------------------- | ---------------------------------------------------------------------------- |
| `dns.zone.read`       | List and view zones, and list zone templates.                                |
| `dns.zone.create`     | Create zones.                                                                |
| `dns.zone.update`     | Change a zone's description or kind, turn DNSSEC on or off, apply templates. |
| `dns.zone.delete`     | Delete zones.                                                                |
| `dns.record.read`     | List and view records.                                                       |
| `dns.record.create`   | Add records.                                                                 |
| `dns.record.update`   | Edit records.                                                                |
| `dns.record.delete`   | Delete records.                                                              |
| `dns.domain.search`   | Check whether a domain name is available.                                    |
| `dns.domain.read`     | List and view registered domains.                                            |
| `dns.domain.register` | Register a domain (charges your balance).                                    |
| `dns.domain.renew`    | Renew a domain (charges your balance).                                       |
| `dns.domain.transfer` | Transfer a domain in (charges your balance).                                 |
| `dns.domain.delete`   | Remove a domain from the tenant's list.                                      |

With the default roles:

- **Tenant owner** and **tenant admin** have every DNS permission.
- **Tenant member** has everything except `dns.zone.delete` and `dns.domain.delete`.
- **Tenant viewer** can read zones, records and domains, and can search domain availability.

See [Roles and permissions](/docs/admin/roles-and-permissions) and the [Permissions reference](/docs/reference/permissions) for the full list.

## Using the API

API requests need an access token and the ID of the tenant you are working in:

```sh
curl https://app.example.com/api/v1/dns/zones \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID"
```

Create a token under **Settings > Access Tokens** (see [Access tokens](/docs/account/access-tokens)). List endpoints accept `limit` (default 50, maximum 200) and `offset` query parameters and return `items`, `total`, `limit` and `offset`.

Errors use the standard envelope:

```json
{
	"error": {
		"code": "bad_request",
		"message": "dns: zone name must be a canonical lowercase DNS name ending with a dot: name \"Example.com.\" must be lowercase"
	}
}
```

If the operator has not enabled DNS on this installation, every DNS endpoint returns `501` with code `not_implemented`, and the dashboard shows "This feature is not enabled".

Every change you make (zone, record, DNSSEC and template actions) is written to the [audit log](/docs/audit/overview).
