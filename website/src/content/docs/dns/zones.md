---
title: Zones
description: Create, list, view, update and delete DNS zones, and understand zone names, kinds and the default SOA and NS records.
---

A zone holds the DNS data for one domain and every name under it. This page shows how to create a zone, what Lahijan sets up for you, and how to change or delete a zone later.

## Create a zone

### In the dashboard

1. Open **DNS** and click **New zone**.
2. Enter the **Zone name**, for example `example.com`. The form lowercases the name and adds the trailing dot for you, so `Example.com` becomes `example.com.`.
3. Optionally add a **Description** ("What is this zone used for?").
4. Pick a **Kind**. Leave it on **Native** unless you know you need something else (see [Zone kinds](#zone-kinds)).
5. Optionally choose a template under **Apply a template**. Its records are added right after the zone is created. See [Zone templates](/docs/dns/templates).
6. Click **Create zone**.

The new zone appears in the list with its name, kind, DNSSEC status and creation date. Click the zone name to open it.

### With the API

`POST /api/v1/dns/zones`

```sh
curl -X POST https://app.example.com/api/v1/dns/zones \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"name": "example.com.", "description": "Company website", "kind": "Native"}'
```

| Field         | Required | Notes                                            |
| ------------- | -------- | ------------------------------------------------ |
| `name`        | Yes      | Canonical zone name: lowercase, ends with a dot. |
| `description` | No       | Free text, shown in the dashboard.               |
| `kind`        | No       | `Native` (default), `Master` or `Slave`.         |

A successful call returns `201` with the zone:

```json
{
	"id": "6f1c2d9e-8a53-4c0b-9a57-0c1f3e2b7d44",
	"tenantId": "0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b",
	"canonicalId": "example.com.",
	"name": "example.com.",
	"kind": "Native",
	"description": "Company website",
	"isDnssecEnabled": false,
	"isAxfrEnabled": false,
	"createdAt": "2026-09-25T10:00:00Z",
	"updatedAt": "2026-09-25T10:00:00Z"
}
```

Unlike the dashboard, the API does not fix up the name for you. Send it in canonical form.

## Zone name rules

The API rejects a zone name with `400 bad_request` when it:

- is empty,
- does not end with a dot (`example.com` fails, `example.com.` works),
- contains spaces, tabs or line breaks,
- contains uppercase letters.

The error message says which rule failed, for example `name "example.com" must end with a dot`.

Zone names are unique across the whole platform. If the name is already hosted by any tenant, creation fails with `409 conflict`.

## What Lahijan sets up for you

When a zone is created, the DNS service seeds it with:

- **NS records** at the zone apex that point at the nameservers your operator configured.
- An **SOA record** built from your operator's default SOA settings. The SOA serial is increased automatically each time you change records through Lahijan.

These seeded records are served on the public internet but are not listed on the zone's **Records** tab, which only shows records you (or a template) added. See [How delegation works](/docs/dns/overview#how-delegation-works) for how to point your domain at these nameservers.

New zones start with DNSSEC off (unless your operator turned it on for all new zones) and zone transfers (AXFR) off.

## Zone kinds

The **Kind** menu offers three choices. The dashboard labels and API values differ:

| Dashboard label | API value | Meaning                                                                                                        |
| --------------- | --------- | -------------------------------------------------------------------------------------------------------------- |
| **Native**      | `Native`  | Lahijan serves the zone from its own database. This is the default and the right choice for almost every zone. |
| **Primary**     | `Master`  | The zone is marked as a primary that other servers may copy from.                                              |
| **Secondary**   | `Slave`   | The zone is marked as a copy of a primary hosted elsewhere.                                                    |

> [!NOTE]
> Lahijan does not give you a way to configure zone transfers (which servers may copy the zone, or where a secondary copies from). Unless your operator has set that up for you, use **Native**.

## List and view zones

The **DNS** page lists your zones. Use the filter box ("Filter by name…") to narrow the list by name or description.

Open a zone to see three tabs:

- **Records**: the records in the zone. See [Records](/docs/dns/records).
- **Templates**: the template catalog, with an **Apply** button per template. See [Zone templates](/docs/dns/templates).
- **Settings**: the DNSSEC card (see [DNSSEC](/docs/dns/dnssec)) and the zone's **Zone id**, **Canonical id**, **Kind** and creation date.

API equivalents:

- `GET /api/v1/dns/zones` lists zones (paginated with `limit` and `offset`).
- `GET /api/v1/dns/zones/{zoneId}` returns one zone. This call also refreshes the `isDnssecEnabled` flag from the live DNS server, so it is the most accurate way to read DNSSEC status.

## Update a zone

You can change a zone's description and kind with the API. The name cannot be changed; to rename, create a new zone and delete the old one.

`PATCH /api/v1/dns/zones/{zoneId}`

```sh
curl -X PATCH https://app.example.com/api/v1/dns/zones/$ZONE_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"description": "Marketing site"}'
```

Both `description` and `kind` are optional; fields you leave out stay unchanged. You need the `dns.zone.update` permission.

## Delete a zone

Deleting a zone removes it from the DNS servers and deletes every record in it. Resolvers stop getting answers for the domain once their cached copies expire.

- **Dashboard:** open **DNS**, click the actions menu on the zone's row and choose **Delete**.
- **API:** `DELETE /api/v1/dns/zones/{zoneId}` returns `204` on success.

> [!CAUTION]
> The dashboard deletes the zone as soon as you click **Delete**. There is no confirmation step and no undo. If the domain is still delegated to Lahijan, it stops resolving. If DNSSEC was on and your registrar still publishes a DS record, remove the DS record first.

You need the `dns.zone.delete` permission. The default **Tenant member** role does not have it.
