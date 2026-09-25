---
title: Billing administration
description: Top up and refund user balances, maintain the price catalog, and manage Stripe plans, promo codes and webhooks.
---

This page is for operators and tenant administrators. It covers the admin side of billing: crediting and refunding balances, the price catalog, and, when Stripe is connected, subscription plans, promo codes and the webhook log. For what users see, read [Balance and usage](/docs/billing/overview) and [Payments and plans](/docs/billing/payments).

## Everything is per tenant

Balances, ledgers, prices, plans and promo codes all belong to a tenant. Every admin billing call acts in the tenant named by the `X-Tenant-Id` header, which in the dashboard is the tenant selected in the tenant switcher.

This has two practical effects:

- A top-up credits the user's balance **in the tenant you are working in**. If a user works in their personal tenant, a top-up made from the default tenant lands in a balance they never see. To credit a balance in another tenant you need a membership there.
- Plans and promo codes created in one tenant are only visible to members of that tenant.

Lahijan does not check that the user id you top up is a member of the tenant.

## Who can use it

| Permission                                    | Allows                                              | Held by          |
| --------------------------------------------- | --------------------------------------------------- | ---------------- |
| `billing.balance.adjust`                      | Top-ups, refunds, balance rebuild                   | Owner, admin     |
| `billing.balance.read`, `billing.ledger.read` | Reading any user's balance and ledger in the tenant | All tenant roles |
| `billing.price_catalog.update`                | Setting prices                                      | Owner, admin     |
| `billing.plan.manage`                         | Creating, editing, deleting and pushing plans       | Owner, admin     |
| `billing.promo_code.manage`                   | Creating, listing and revoking promo codes          | Owner, admin     |
| `billing.webhook.read`                        | Reading the Stripe webhook log                      | Owner, admin     |

`platform.admin` passes all of these in its tenant. The dashboard shows **Administration > Billing** only to `platform.admin`, but the API accepts any role that holds the permission. In particular, the owner of a personal tenant can top up their own balance in that tenant; see [Roles and permissions](/docs/admin/roles-and-permissions#tenant-owner).

## Top up or refund a user in the dashboard

1. Switch to the tenant whose balance you want to change.
2. Open **Administration > Billing** and select the **Users** tab.
3. Paste the user's id into **User id**. Users can find their id in `GET /api/v1/auth/me`; you can also look it up in the database (see [Users and tenants](/docs/admin/users-and-tenants#manage-members-in-the-database)).
4. The card shows the user's current balance. Select **Top up** or **Refund**.
5. Enter **Amount (¢)** in cents, a **Currency** and an optional **Reference** (for example an invoice number), then confirm.
6. Select **User ledger** to see every entry for that user.

Both actions add a credit row to the ledger (source **Top-up** or **Refund**). The ledger is append-only, so a mistaken top-up cannot be deleted. There is no manual debit in this release.

## Top up or refund with the API

```sh
curl -X POST https://cloud.example.com/api/v1/admin/users/<user id>/topup \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"amountCents": 10000, "currency": "USD", "reference": "INV-2026-041"}'
```

| Method and path                             | Body                                                                                            | Result                                                        |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `POST /api/v1/admin/users/{userId}/topup`   | `amountCents` (required, greater than 0), `currency`, `reference`                               | The new ledger row (`201`).                                   |
| `POST /api/v1/admin/users/{userId}/refund`  | `amountCents` (required), `currency`, `reference`, `chargeLedgerId` (the charge being refunded) | The new ledger row (`201`).                                   |
| `GET /api/v1/admin/users/{userId}/ledger`   | `limit`, `offset` query parameters                                                              | The user's ledger entries.                                    |
| `GET /api/v1/admin/users/{userId}/balance`  |                                                                                                 | The user's cached balance.                                    |
| `POST /api/v1/admin/users/{userId}/balance` |                                                                                                 | Recomputes the cached balance from the ledger and returns it. |

Currency defaults to `USD`, the only currency in this release. Each top-up, refund and rebuild is recorded in the audit log (`billing.balance.topup`, `billing.balance.refund`, `billing.balance.rebuild`) before it runs and marked with its outcome afterwards.

## Price catalog

The price catalog holds a price per resource type and unit, for example `compute.cpu` per `vCPU-hour`. In the dashboard it is the **Price catalog** tab; select **Set price** and fill in **Resource type**, **Unit**, **Price (¢)**, **Currency** and **Effective from**.

With the API, `GET /api/v1/admin/billing/prices` lists prices (needs `billing.price_catalog.read`, which every tenant role has) and `POST /api/v1/admin/billing/prices` sets one:

```json
{
	"resourceType": "compute.cpu",
	"unit": "vCPU-hour",
	"priceCents": 2,
	"currency": "USD",
	"effectiveFrom": "2026-10-01T00:00:00Z"
}
```

Setting a price closes the price currently in effect for that resource type and unit and starts the new one at `effectiveFrom` (now, if omitted). Old prices are kept with their end date.

> [!NOTE]
> Resource metering is not implemented in this release, so no charges are calculated from the price catalog yet. Domain orders are priced by the registrar configuration, not by this catalog.

## Stripe

Card payments, plans and promo codes need the Stripe gateway. Turn it on in the configuration:

| Key                             | Environment variable                    | Purpose                                                 |
| ------------------------------- | --------------------------------------- | ------------------------------------------------------- |
| `billing.stripe.enabled`        | `LAHIJAN_BILLING_STRIPE_ENABLED`        | Turns the gateway on.                                   |
| `billing.stripe.secretKey`      | `LAHIJAN_BILLING_STRIPE_SECRETKEY`      | `sk_test_...` or `sk_live_...`.                         |
| `billing.stripe.publishableKey` | `LAHIJAN_BILLING_STRIPE_PUBLISHABLEKEY` | Given to browsers through `GET /api/v1/billing/config`. |
| `billing.stripe.webhookSecret`  | `LAHIJAN_BILLING_STRIPE_WEBHOOKSECRET`  | `whsec_...`, used to verify webhook signatures.         |

The production compose file does not pass these variables to the `lahijan` service, so add them to that service's `environment` block (or set the keys in a mounted configuration file). See [Environment file](/docs/operations/environment).

In the Stripe dashboard, add a webhook endpoint pointing at `https://<your server>/api/v1/webhooks/stripe`. Lahijan acts on these event types:

| Event                                                            | Effect                                                          |
| ---------------------------------------------------------------- | --------------------------------------------------------------- |
| `payment_intent.succeeded`                                       | Credits the top-up to the user's ledger.                        |
| `invoice.paid`                                                   | Credits the subscription plan's included amount.                |
| `customer.subscription.created`, `customer.subscription.updated` | Marks the local subscription active and updates its period end. |
| `customer.subscription.deleted`                                  | Marks the local subscription expired.                           |
| `charge.dispute.created`, `charge.dispute.closed`                | Recorded only; no ledger change yet.                            |

Other event types are recorded and ignored. Each event is processed once, keyed on its Stripe event id. When the gateway is off, the webhook endpoint answers `200` and discards the body so Stripe stops retrying.

See the full list of settings in [Configuration](/docs/reference/configuration).

### Plans

| Method and path                                  | Purpose                                                                                               |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------- |
| `GET /api/v1/admin/billing/plans`                | Every plan in the tenant, active or not (`billing.plan.read`).                                        |
| `POST /api/v1/admin/billing/plans`               | Creates a plan.                                                                                       |
| `GET /api/v1/admin/billing/plans/{planId}`       | One plan.                                                                                             |
| `PATCH /api/v1/admin/billing/plans/{planId}`     | Changes a plan's editable fields.                                                                     |
| `DELETE /api/v1/admin/billing/plans/{planId}`    | Deletes a plan. Fails with `409` if any subscription uses it; set `active` to `false` instead.        |
| `POST /api/v1/admin/billing/plans/{planId}/push` | Creates the matching Product and Price in Stripe and stores their ids. Running it again does nothing. |

```json
{
	"slug": "starter",
	"name": "Starter",
	"interval": "monthly",
	"priceCents": 1000,
	"currency": "USD",
	"includedQuotaCents": 1200,
	"active": true,
	"sortOrder": 1
}
```

`slug`, `name`, `interval` (`monthly` or `yearly`) and `priceCents` are required. `includedQuotaCents` is credited to the subscriber's balance on each paid invoice. A plan must be pushed to Stripe before anyone can subscribe to it; until then, subscribing fails with `409`. The `overageDiscountPercent` field is stored but has no effect while metering is inactive.

### Promo codes

| Method and path                                               | Purpose                                         |
| ------------------------------------------------------------- | ----------------------------------------------- |
| `GET /api/v1/admin/billing/promo-codes`                       | Lists codes that are not revoked.               |
| `POST /api/v1/admin/billing/promo-codes`                      | Creates a code.                                 |
| `POST /api/v1/admin/billing/promo-codes/{promoCodeId}/revoke` | Revokes a code so it can no longer be redeemed. |

```json
{
	"code": "WELCOME-25",
	"creditCents": 2500,
	"currency": "USD",
	"note": "Launch campaign",
	"maxUses": 100,
	"expiresAt": "2026-12-31T23:59:59Z"
}
```

`code` and `creditCents` are required. Codes may contain letters, digits and dashes. `appliesToPlanId` is stored but not checked at redemption in this release, so a plan-restricted code can be redeemed by anyone in the tenant.

### Webhook log

`GET /api/v1/admin/billing/webhook-events` lists recently received Stripe events with their type, processing status, the ledger entries they created and any error message. Use it to check that Stripe can reach Lahijan and that payments are being credited.

## Agent charges

`agent.billing.centsPer1kTokens` sets a price per 1,000 model tokens for Agent turns that run on operator-provided models. Operator-provided models are not available yet, and turns that use a user's own key are never charged, so this setting currently has no effect. See [Agent policy](/docs/admin/agent-policy).
