---
title: Domains
description: Search for, register, renew and transfer domain names through Lahijan's domain API, and understand how charges and the domain list work.
---

The Domains API lets you buy and manage domain names through your operator's registrar account, paying from your Lahijan balance. It covers four actions: check availability, register, renew and transfer in. Hosting the DNS for a domain is a separate step, done with [Zones](/docs/dns/zones).

> [!NOTE]
> Domain registration is optional and off by default. Your operator must connect a registrar account before it works. When it is off, every `/api/v1/dns/domains` endpoint returns `501` with code `not_implemented`.
>
> There is no dashboard page for domains yet. Use the REST API.

All examples use your access token and tenant ID, as described in the [DNS overview](/docs/dns/overview#using-the-api).

## Domain name format

Send domain names **without** a trailing dot, in lowercase, with no spaces: `example.com`, not `example.com.` or `Example.com`. Otherwise the request fails with `400 bad_request`. Responses show stored names **with** a trailing dot (`example.com.`).

## Check availability

`POST /api/v1/dns/domains/search`

```sh
curl -X POST https://app.example.com/api/v1/dns/domains/search \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"domain": "example.com"}'
```

Example response:

```json
{
	"domain": "example.com",
	"available": true,
	"status": "available",
	"pricing": [
		{ "periodYears": 1, "priceCents": 1500, "currency": "USD" },
		{ "periodYears": 2, "priceCents": 3000, "currency": "USD" }
	]
}
```

- `status` is `available` or `unavailable`. `reason` may explain why a name is unavailable.
- `pricing` lists the price for each registration period the registrar offers, in integer cents. Prices already include any margin your operator adds.

Each search is recorded in the [audit log](/docs/audit/overview), because registrars may bill the operator per lookup. Searching needs `dns.domain.search`, which every default role has.

## Register a domain

`POST /api/v1/dns/domains`

```sh
curl -X POST https://app.example.com/api/v1/dns/domains \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "domain": "example.com",
    "periodYears": 1,
    "autoRenew": false,
    "whoisPrivacy": true,
    "contact": {
      "ownerFirstname": "Sara",
      "ownerLastname": "Ahmadi",
      "ownerOrganization": "Example Ltd",
      "ownerEmail": "sara@example.org",
      "ownerPhone": "+982112345678",
      "address1": "1 Main Street",
      "city": "Tehran",
      "state": "Tehran",
      "zip": "1234567890",
      "countryCode": "IR"
    }
  }'
```

| Field           | Required         | Notes                                                                                                                                                                                                                                                                       |
| --------------- | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `domain`        | Yes              | The name to register.                                                                                                                                                                                                                                                       |
| `periodYears`   | Yes              | 1 to 10. Must match a period the registrar prices.                                                                                                                                                                                                                          |
| `contact`       | In practice, yes | The registrant contact. Owner first and last name, email, phone, `address1`, `city`, `state`, `zip` and `countryCode` are required; leaving the contact out fails with `contact profile is incomplete`. `ownerPhone` uses E.164 style, `countryCode` is ISO 3166-1 alpha-2. |
| `autoRenew`     | No               | Passed to the registrar. Default `false`.                                                                                                                                                                                                                                   |
| `whoisPrivacy`  | No               | Passed to the registrar. Default `false`.                                                                                                                                                                                                                                   |
| `autoProvision` | No               | Accepted, but Lahijan does not currently create a zone for you.                                                                                                                                                                                                             |
| `autoDNSSEC`    | No               | Accepted, but Lahijan does not currently sign the zone or publish a DS record.                                                                                                                                                                                              |

What happens:

1. Lahijan checks availability again. If the name is taken, you get `409 conflict`. If the registrar has no price for the requested period, you get `400 bad_request`.
2. The price for that period is charged to your balance as a ledger entry. Lahijan does not check that your balance covers the charge first.
3. The order is placed with the registrar. If the registrar rejects it, Lahijan adds a refund entry that reverses the charge.
4. On success you get `201` with the domain record.

After registering, create a zone for the domain (see [Zones](/docs/dns/zones)) and make sure the domain's nameservers point to your operator's nameservers (see [How delegation works](/docs/dns/overview#how-delegation-works)).

## The domain record

Each registered, renewed or transferred domain appears in your tenant's list:

| Field                       | Meaning                                                                                         |
| --------------------------- | ----------------------------------------------------------------------------------------------- |
| `name`                      | The domain, with a trailing dot.                                                                |
| `status`                    | `available`, `registered`, `pending`, `transferred` or `expired`, as reported by the registrar. |
| `priceCents`, `currency`    | The amount charged at registration or transfer.                                                 |
| `periodYears`               | The registration period ordered.                                                                |
| `isAutoRenew`               | Whether auto-renew was requested.                                                               |
| `registeredAt`, `expiresAt` | Dates reported by the registrar.                                                                |
| `ledgerEntryId`             | The ledger entry for the most recent charge.                                                    |
| `zoneId`                    | Reserved for a linked zone. Currently always empty.                                             |

List and view them with:

- `GET /api/v1/dns/domains` (paginated with `limit` and `offset`)
- `GET /api/v1/dns/domains/{domainId}`

These need `dns.domain.read`.

## Renew a domain

`POST /api/v1/dns/domains/{domainId}/renew`

```sh
curl -X POST https://app.example.com/api/v1/dns/domains/$DOMAIN_ID/renew \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"periodYears": 1}'
```

`periodYears` is 1 to 10. The price is worked out from the per-year price stored when the domain was registered. The charge is posted first and reversed with a refund entry if the registrar refuses the renewal. The response (`200`) is the domain record with the new `expiresAt`.

## Transfer a domain in

`POST /api/v1/dns/domains/transfer`

```sh
curl -X POST https://app.example.com/api/v1/dns/domains/transfer \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"domain": "example.com", "authCode": "Xy7-auth-code", "periodYears": 1}'
```

| Field         | Required | Notes                                                                                           |
| ------------- | -------- | ----------------------------------------------------------------------------------------------- |
| `domain`      | Yes      | The domain to move to your operator's registrar.                                                |
| `authCode`    | Yes      | The transfer (EPP) code from your current registrar.                                            |
| `periodYears` | Yes      | 1 to 10.                                                                                        |
| `contact`     | No       | Same shape as for registration. Include it; without it the registrar receives an empty contact. |

Before you start, remove the transfer lock at your current registrar. If the registrar refuses the transfer (for example because the domain is still locked or the code is wrong), the request fails and the charge is refunded.

Transfers are charged a fixed 10.00 per year in the platform's billing currency, plus any operator margin. The response is `201` with the domain record; its `status` is usually `pending` until the transfer completes.

## Remove a domain from the list

`DELETE /api/v1/dns/domains/{domainId}` returns `204`.

This only removes the domain from your tenant's list in Lahijan. It does **not** cancel the registration or stop auto-renewal at the registrar, and nothing is refunded. It needs `dns.domain.delete`, which the default **Tenant member** role does not have.

## Errors

| Status | Code              | When                                                                                                                |
| ------ | ----------------- | ------------------------------------------------------------------------------------------------------------------- |
| `400`  | `bad_request`     | Bad name format, `periodYears` outside 1 to 10, incomplete contact, missing `authCode`, or no price for the period. |
| `404`  | `not_found`       | The domain is not in this tenant's list.                                                                            |
| `409`  | `conflict`        | You tried to register a name that is not available.                                                                 |
| `501`  | `not_implemented` | Domain registration is not enabled on this installation.                                                            |

Charges and refunds appear in your ledger. See [Balance and usage](/docs/billing/overview).
