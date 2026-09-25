---
title: Balance and usage
description: How your Lahijan balance, ledger, usage records and receipts work, and what is charged today.
---

Lahijan bills from a prepaid balance. Money is added to your balance as credits (a top-up, a refund, a redeemed code) and taken away as debits (charges). Every credit and debit is a row in your ledger, and your balance is the sum of the ledger. This page explains what you can see and what is actually charged in the current release.

## Balances are per tenant

You have a separate balance in each tenant you belong to. The balance, ledger, usage and receipts you see are always for the tenant currently selected in the dashboard's tenant switcher (or named in the `X-Tenant-Id` header when you use the API). A top-up in one tenant does not show up in another.

All amounts are whole cents of a single currency, USD in this release. For example `2500` means 25.00 USD.

## The Billing page

Open **Billing** in the sidebar. The page has four parts:

- **Balance**: your current balance, when the last ledger entry was recorded and when the figure was refreshed.
- **Usage**: your usage records for the **Last 7 days** or **Last 30 days**, grouped by resource type.
- **Receipts**: PDF summaries for a period, with **Download PDF**.
- **Ledger**: every credit and debit, newest first, with **Type**, **Source**, **Amount**, **Currency**, **Reference** and **At**. Select **Load more** for older rows.

## The ledger

The ledger is append-only: rows are never edited or deleted. A mistake is corrected by adding a new row, for example a refund.

| Type            | Source     | What it means                                                                       |
| --------------- | ---------- | ----------------------------------------------------------------------------------- |
| Credit          | Top-up     | Money added by an administrator, a card payment or a redeemed code.                 |
| Credit          | Refund     | Money given back by an administrator.                                               |
| Debit           | Charge     | Something you used or bought.                                                       |
| Credit or debit | Adjustment | Reserved for manual corrections. Nothing in the current release writes this source. |

The balance shown on the page is a cached total that is refreshed after every ledger write. If it ever looks wrong, an administrator can rebuild it from the ledger.

## What is charged today

Be aware of what the current release does and does not bill:

| Item                                                         | Charged?                                                                                                           |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| Compute instances, DNS zones and records, object storage     | No. Per-minute metering of these resources is not implemented yet, so they create no usage records and no charges. |
| Registering, renewing or transferring a domain               | Yes. The price is debited from your balance when you order. See [Domains](/docs/dns/domains).                      |
| The [Agent](/docs/agent/overview) with your own provider key | No. You pay your model provider directly.                                                                          |

Because resource metering is not active, the **Usage** panel is normally empty. Your operator may still publish a price catalog; it has no effect until metering ships.

## Low and negative balances

A charge is recorded even if it takes your balance below zero. In the current release nothing is stopped or blocked when your balance reaches zero: running instances keep running, and there is no automatic grace-period shutdown. Your operator may contact you or adjust your balance by hand.

There are no spend limits on your account other than the agent limits a tenant administrator can set on [Agent policy](/docs/admin/agent-policy).

## Receipts

A receipt is a PDF covering a period you choose. It shows the total of the charges in that period and your balance at the end of it.

To create one, go to **Receipts**, fill in **Period start** and **Period end**, and select **Generate**. Generating a receipt for the same period again replaces its PDF. Receipts are not created automatically.

Generating receipts needs the `billing.receipt.create` permission, which tenant owners and administrators have but members and viewers do not. Anyone with `billing.receipt.read` can list and download existing receipts.

## Use the API

All of these endpoints need a tenant header. See [Access tokens](/docs/account/access-tokens#tenant-header).

| Method and path                           | What it returns                                                               |
| ----------------------------------------- | ----------------------------------------------------------------------------- |
| `GET /api/v1/me/balance`                  | Your cached balance (`balanceCents`, `currency`, `lastEntryAt`, `updatedAt`). |
| `GET /api/v1/me/ledger`                   | Your ledger entries, newest first.                                            |
| `GET /api/v1/me/usage`                    | Your usage records. Filters: `resourceType`, `from`, `to`.                    |
| `GET /api/v1/me/receipts`                 | Your receipts, newest period first.                                           |
| `POST /api/v1/me/receipts`                | Generates a receipt for `periodStart` to `periodEnd`.                         |
| `GET /api/v1/me/receipts/{receiptId}`     | One receipt's metadata.                                                       |
| `GET /api/v1/me/receipts/{receiptId}.pdf` | The receipt PDF.                                                              |

List endpoints take `limit` (1 to 200, default 50) and `offset`.

```sh
curl https://cloud.example.com/api/v1/me/balance \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID"
```

```json
{
	"tenantId": "0b6a3c3e-7c1d-4c55-9f3a-7f0d2b1f5a10",
	"userId": "8d0f4f7e-2a41-4c4b-8f55-4c1e3f0a9b22",
	"balanceCents": 2500,
	"currency": "USD",
	"lastEntryAt": "2026-09-20T08:12:00Z",
	"updatedAt": "2026-09-20T08:12:00Z"
}
```

Generate a receipt for September:

```sh
curl -X POST https://cloud.example.com/api/v1/me/receipts \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"periodStart": "2026-09-01T00:00:00Z", "periodEnd": "2026-10-01T00:00:00Z"}'
```

`periodEnd` must be after `periodStart`, or the request fails with `400`.

## Who can see what

Tenant viewers, members, administrators and owners can all read their own balance, ledger, usage and receipts. See [Roles and permissions](/docs/admin/roles-and-permissions) for the full breakdown.

To add money to your balance, see [Payments and plans](/docs/billing/payments).
