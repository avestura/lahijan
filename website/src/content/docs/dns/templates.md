---
title: Zone templates
description: Add a predefined set of DNS records to a zone in one step, using the built-in templates for common email and verification setups.
---

Zone templates add a ready-made set of records to a zone, so you do not have to type them one by one. Lahijan ships a small built-in catalog for common email and verification setups. This page lists what each template adds and how applying one behaves.

## Built-in templates

The catalog is fixed. You cannot add, edit or delete templates.

| Template (ID)                                        | Type  | Content                                              |
| ---------------------------------------------------- | ----- | ---------------------------------------------------- |
| Google Workspace setup (`google-workspace`)          | `MX`  | `1 aspmx.l.google.com.`                              |
| Google Workspace setup (`google-workspace`)          | `TXT` | `"v=spf1 include:_spf.google.com ~all"`              |
| Microsoft 365 setup (`microsoft-365`)                | `MX`  | `0 <your-domain>.mail.protection.outlook.com.`       |
| Microsoft 365 setup (`microsoft-365`)                | `TXT` | `"v=spf1 include:spf.protection.outlook.com ~all"`   |
| Google Search Console verification (`verify-google`) | `TXT` | `"google-site-verification=REPLACE-WITH-YOUR-TOKEN"` |

Every template record is created at the zone apex (the zone name itself) with a TTL of 3600 seconds. For `microsoft-365`, `<your-domain>` is your zone name without the trailing dot, so for `example.com.` the MX target is `example.com.mail.protection.outlook.com.`.

> [!NOTE]
> The Google Workspace template adds only the MX and SPF records, despite mentioning DKIM in its description. Add the DKIM `TXT` record from your Google admin console yourself.

After applying **Google Search Console verification**, edit the TXT record and replace `REPLACE-WITH-YOUR-TOKEN` with the token from the verification wizard. See [Edit a record](/docs/dns/records#edit-a-record).

## Apply a template

### When you create a zone

In the **New zone** dialog, choose a template under **Apply a template** ("Predefined records will be added after creation."). The records are added right after the zone is created. If applying the template fails, the zone is still created and you can apply the template again from the zone's **Templates** tab.

### To an existing zone

1. Open the zone and go to the **Templates** tab.
2. Click **Apply** next to the template.

The dashboard reports how many records were added, for example "2 records upserted." The **Apply** button is disabled if you do not have the `dns.record.create` permission.

### With the API

List the catalog with `GET /api/v1/dns/templates`:

```sh
curl https://app.example.com/api/v1/dns/templates \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID"
```

The response is `{"items": [...]}`. Each item has an `id`, `name`, `description` and a `records` list. In record names and content, `%s` stands for the zone name with its trailing dot and `%w` for the zone name without it.

Apply one with `POST /api/v1/dns/zones/{zoneId}/apply-template`:

```sh
curl -X POST https://app.example.com/api/v1/dns/zones/$ZONE_ID/apply-template \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"templateId": "google-workspace"}'
```

The response is `{"applied": 2}`, the number of records added. An unknown `templateId` returns `404 not_found`.

Listing templates needs `dns.zone.read`; applying one through the API needs `dns.zone.update`.

## How applying works

- Each template record goes through the same checks as a record you add by hand (see [Records](/docs/dns/records)), and each one shows up in the audit log as its own record creation, plus one `dns.zone.template.apply` entry for the template.
- Records added by a template are ordinary records. You can edit or delete them like any other.
- Applying the same template twice is safe. Records that already exist with the same name, type and content are skipped and not counted in `applied`.
- If one record fails, the ones before it stay in the zone and the call returns an error.

> [!WARNING]
> Lahijan serves one value per name and type (see [One value per name and type](/docs/dns/records#one-value-per-name-and-type)). Applying a template replaces any `MX` or `TXT` record you already serve at the zone apex. If you use an apex TXT record for something else (another verification token or an existing SPF record), applying a template that adds an apex TXT record takes it out of service. Check the **Records** tab afterwards and merge values into a single record where needed.
