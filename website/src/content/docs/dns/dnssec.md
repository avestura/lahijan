---
title: DNSSEC
description: Turn DNSSEC signing on or off for a zone, find the DS record your registrar needs, and understand what the DNSSEC status means.
---

DNSSEC adds cryptographic signatures to your zone so resolvers can check that answers really came from your nameservers and were not changed on the way. Lahijan can sign any zone you host. This page explains how to turn it on, how to finish the setup at your registrar, and how to turn it off safely.

## What "DNSSEC On" means

Each zone shows a DNSSEC badge: **On** or **Off**. You see it in the zone list on the **DNS** page and in the DNSSEC card on the zone's **Settings** tab.

**On** means Lahijan holds a signing key for the zone and the DNS servers are signing its answers. It does **not** mean resolvers already validate your zone. Validation only starts once the parent zone (for example `.com`) publishes a DS record for your domain, which you arrange through your registrar.

The status comes from the DNS server itself. Opening a zone (`GET /api/v1/dns/zones/{zoneId}`) re-reads it, so the value there is current. The zone list shows the last known value.

New zones start with DNSSEC off, unless your operator configured it to be on for every new zone.

## Turn DNSSEC on

### In the dashboard

1. Open the zone and go to the **Settings** tab.
2. In the DNSSEC card, click **Enable DNSSEC**.
3. Read the confirmation ("Enable DNSSEC?") and click **Confirm**.

The badge changes to **On** and you see "DNSSEC enabled."

### With the API

`POST /api/v1/dns/zones/{zoneId}/dnssec/enable`

```sh
curl -X POST https://app.example.com/api/v1/dns/zones/$ZONE_ID/dnssec/enable \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID"
```

The call has no body and returns `204`. Calling it on a zone that is already signed does nothing, so a repeated click is safe.

You need the `dns.zone.update` permission for both enable and disable.

## Key details

When you enable DNSSEC, Lahijan creates one **combined signing key** (CSK) for the zone. A CSK signs both the zone's key set and its records, so there is a single key and a single DS record to manage.

| Property  | Value                                                 |
| --------- | ----------------------------------------------------- |
| Key type  | Combined signing key (CSK)                            |
| Algorithm | ECDSA P-256 with SHA-256 (DNSSEC algorithm number 13) |
| Key size  | 256 bits                                              |

Lahijan does not rotate the key automatically, and there is no rollover button in the dashboard or the API.

## Give the DS record to your registrar

After you enable DNSSEC, your registrar must publish a DS record for your domain in the parent zone. The DS record is a hash of your zone's public key.

Lahijan does not currently show the DS record in the dashboard or return it from the API. You can build it from the public key your nameservers publish:

1. Query the zone's DNSKEY record from one of your operator's nameservers:

   ```sh
   dig DNSKEY example.com @<one-of-your-operator-nameservers> +multiline
   ```

2. Generate the DS record from it. With the BIND utilities installed:

   ```sh
   dig DNSKEY example.com @<one-of-your-operator-nameservers> | dnssec-dsfromkey -a SHA-256 -f - example.com
   ```

   The output has the form `example.com. IN DS <key tag> 13 2 <digest>`.

3. In your registrar's control panel, add a DS record with that **key tag**, **algorithm** `13`, **digest type** `2` (SHA-256) and **digest**. Some registrars ask for the DNSKEY instead; in that case paste the public key from step 1 with algorithm `13`.

If you cannot run these tools, ask your operator for the DS record of your zone.

Once the registry publishes the DS record, validating resolvers start checking your signatures. You can test the chain with an online DNSSEC analyzer or with `dig +dnssec example.com`.

> [!NOTE]
> If you registered the domain through Lahijan's [Domains](/docs/dns/domains) API, the `autoDNSSEC` option is accepted but does not currently sign the zone or publish a DS record. Enable DNSSEC on the zone yourself and follow the steps above.

## Turn DNSSEC off

Turning DNSSEC off deletes every signing key for the zone. The zone is then served unsigned.

> [!CAUTION]
> Remove the DS record at your registrar **first**, then wait for the parent zone's DS TTL to pass (often a day or two), and only then disable DNSSEC here. If you disable DNSSEC while the parent still publishes a DS record, validating resolvers treat your domain as broken and stop answering for it. The old key cannot be recovered; turning DNSSEC on again creates a new key with a new DS record.

- **Dashboard:** on the zone's **Settings** tab, click **Disable DNSSEC**, then **Confirm**.
- **API:** `POST /api/v1/dns/zones/{zoneId}/dnssec/disable` (no body, returns `204`). Calling it on an unsigned zone does nothing.

## Deleting a signed zone

Deleting a zone also deletes its keys. As with disabling, remove the DS record at your registrar before you delete a signed zone that is still delegated to Lahijan.

DNSSEC changes are recorded in the [audit log](/docs/audit/overview) as `dns.zone.dnssec.enable` and `dns.zone.dnssec.disable`.
