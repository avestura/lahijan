---
title: Records
description: Add, edit, disable and delete DNS records, with the supported record types, content formats, TTL limits and the validation errors you may see.
---

Records are the entries in a zone that answer DNS queries: the address of `www`, the mail servers for the domain, verification tokens and so on. This page covers the supported types, the format each one expects, and how editing and deleting behave.

Open a zone under **DNS** and use the **Records** tab. You can filter the list by name or content ("Filter by name or content…") and by type (**All types** or one type).

## Add a record

### In the dashboard

1. On the **Records** tab, click **Add record**.
2. Fill in **Name**. You can type a short label (`www`), `@` or nothing for the zone apex, or a full name (`www.example.com` or `www.example.com.`). The dashboard turns it into the full canonical name for you.
3. Pick the **Type**.
4. Enter the **Content** in the format for that type (see the table below).
5. Set **TTL (seconds)**. The default is 3600.
6. Tick **Disabled (served commented-out)** if you want to create the record without serving it yet.
7. Click **Add**.

### With the API

`POST /api/v1/dns/zones/{zoneId}/records`

```sh
curl -X POST https://app.example.com/api/v1/dns/zones/$ZONE_ID/records \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"name": "www.example.com.", "type": "A", "content": "203.0.113.10", "ttl": 3600}'
```

| Field      | Required | Notes                                                                                                   |
| ---------- | -------- | ------------------------------------------------------------------------------------------------------- |
| `name`     | Yes      | Full canonical name: lowercase, ends with a dot, inside the zone. The API does not expand short labels. |
| `type`     | Yes      | One of the types in the table below.                                                                    |
| `content`  | Yes      | The value, in the format for the type.                                                                  |
| `ttl`      | No       | Seconds, from 300 to 86400. Defaults to 3600 when left out.                                             |
| `disabled` | No       | `true` stores the record but does not serve it. Defaults to `false`.                                    |

The response (`201`) is the stored record, including a `prio` field for `MX` and `SRV` records, taken from the content.

## Supported record types

| Type    | Content format                                                                                                                      | Example                                                             |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `A`     | An IPv4 address.                                                                                                                    | `203.0.113.10`                                                      |
| `AAAA`  | An IPv6 address.                                                                                                                    | `2001:db8::10`                                                      |
| `CNAME` | A canonical name (ends with a dot). Not allowed at the zone apex.                                                                   | `app.example.net.`                                                  |
| `MX`    | `<priority> <mail server>`. Priority 0 to 65535; the server name ends with a dot.                                                   | `10 mail.example.com.`                                              |
| `TXT`   | A quoted string. Inner quotes must be escaped with a backslash.                                                                     | `"v=spf1 include:_spf.example.net ~all"`                            |
| `NS`    | A canonical name.                                                                                                                   | `ns1.example.net.`                                                  |
| `SRV`   | `<priority> <weight> <port> <target>`. Each number 0 to 65535; the target ends with a dot.                                          | `10 60 5060 sip.example.com.`                                       |
| `CAA`   | `<flags> <tag> "<value>"`. Flags 0 to 255.                                                                                          | `0 issue "letsencrypt.org"`                                         |
| `PTR`   | A canonical name.                                                                                                                   | `host1.example.com.`                                                |
| `DS`    | `<key tag> <algorithm> <digest type> <digest>`. Key tag 0 to 65535, algorithm and digest type 0 to 255, digest as even-length hex.  | `12345 13 2 3a1f...`                                                |
| `TLSA`  | `<usage> <selector> <matching type> <certificate data>`. Numbers 0 to 255, data as even-length hex.                                 | `3 1 1 0c72ac...`                                                   |
| `SOA`   | `<primary ns> <contact> <serial> <refresh> <retry> <expire> <minimum>`. Names end with a dot; numbers are 32-bit unsigned integers. | `ns1.example.com. hostmaster.example.com. 1 10800 3600 604800 3600` |

Names inside content (`CNAME`, `MX`, `NS`, `SRV`, `PTR`, `SOA`) must be lowercase and end with a dot. `A` and `AAAA` addresses are stored in their normalized form, so `2001:0db8:0000::10` is saved as `2001:db8::10`.

## TTL

The TTL tells resolvers how long they may cache an answer. Lahijan accepts values from **300** (5 minutes) to **86400** (1 day). Anything outside that range is rejected with `ttl must be in [300, 86400]`.

Lower the TTL a day before you plan to change an important record, so the change takes effect quickly. Raise it again afterwards.

## One value per name and type

Records with the same name and type form a set (an "RRset"). Lahijan currently writes each record as the **entire** set for its name and type. In practice:

- **Adding** a record replaces whatever was being served for that name and type. If you add a second `A` record for `www.example.com.`, only the newest one is served, even though both appear on the **Records** tab.
- **Deleting** a record removes every served value for that name and type.

Keep one record per name and type. If you need to change a value, edit the existing record instead of adding another.

> [!WARNING]
> Do not add `NS` or `SOA` records at the zone apex. Lahijan already serves the operator's nameservers and SOA for every zone, and a new record at the apex replaces them. Getting them wrong can take the whole domain offline. `NS` records for a subdomain (delegating `sub.example.com.` elsewhere) are fine.

## Edit a record

You can change a record's content, TTL and disabled flag. The name and type cannot be changed; delete the record and create a new one instead.

- **Dashboard:** click **Save** on the record's row to open **Edit record**, change **Content**, **TTL (seconds)** or **Disabled**, then click **Save**.
- **API:** `PATCH /api/v1/dns/zones/{zoneId}/records/{recordId}` with any of `content`, `ttl` and `disabled`:

```sh
curl -X PATCH https://app.example.com/api/v1/dns/zones/$ZONE_ID/records/$RECORD_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"content": "203.0.113.20", "ttl": 600}'
```

The new content is validated with the same rules as on creation.

### Disable a record

A disabled record stays in Lahijan but is not served. The **Disabled** column shows **Yes** for these records. Use it to take a record out of service without losing its value.

## Delete records

- **One record:** click the trash icon on its row, or call `DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}` (returns `204`).
- **Several records:** tick the checkboxes on the rows, then click **Delete selected**.

The dashboard deletes immediately, without a confirmation step. The change is live on the DNS servers as soon as the call succeeds; resolvers that cached the old answer keep it until the TTL runs out.

## Validation errors

Lahijan checks every record before it reaches the DNS servers and returns `400 bad_request` with a message that names the problem. Common ones:

| Message (shortened)                                | Cause                                                                                       |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `A record content must be a valid IPv4 address`    | The content is not an IPv4 address, or is IPv6.                                             |
| `AAAA record content must be IPv6, got IPv4`       | You used an IPv4 address in an `AAAA` record.                                               |
| `TXT content must be a quoted string`              | The TXT value is missing its surrounding double quotes.                                     |
| `TXT content has an unescaped quote`               | A `"` inside the value is not written as `\"`.                                              |
| `MX content must be "<priority> <canonical-name>"` | The priority or server name is missing.                                                     |
| `must end with a dot (canonical form)`             | A name in the content has no trailing dot.                                                  |
| `must be lowercase (canonical form)`               | A name contains uppercase letters.                                                          |
| `name "..." does not belong to zone "..."`         | The record name is outside the zone (API only; the dashboard builds names inside the zone). |
| `CNAME at zone apex is not allowed (strict RFC)`   | You tried to add a `CNAME` for the zone name itself. Use `A`/`AAAA` records there.          |
| `ttl must be in [300, 86400]`                      | The TTL is out of range.                                                                    |
| `record type is not supported`                     | The type is not in the table above.                                                         |

Other errors:

- `409 conflict`: a record with the same name, type and content already exists in the zone.
- `404 not_found`: the zone or record does not exist in this tenant.

Record changes are recorded in the [audit log](/docs/audit/overview).
