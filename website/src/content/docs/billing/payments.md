---
title: Payments and plans
description: The ways money gets into your Lahijan balance: administrator top-ups, card payments through Stripe, subscription plans and promo codes.
---

Your balance goes up in one of four ways: an administrator tops it up, you pay by card, a subscription plan credits its included amount, or you redeem a promo code. The first always works. The other three need your operator to connect Stripe, and in the current release they are available through the API only; the dashboard has no payment screens yet.

## Check what your server supports

`GET /api/v1/billing/config` tells you whether card payments are turned on. It does not need a token.

```sh
curl https://cloud.example.com/api/v1/billing/config
```

```json
{ "enabled": true, "publishableKey": "pk_live_...", "liveMode": true }
```

When `enabled` is `false`, every endpoint on this page except `GET /api/v1/billing/config` and `GET /api/v1/billing/plans` answers `501` ("feature disabled"), and the plan list is empty. Balances still work; ask your administrator for a top-up.

## Top-up by an administrator

Without card payments, an administrator adds credit to your balance by hand. It appears in your ledger as a credit with the source **Top-up** and the reference the administrator entered. Top-ups land in the tenant the administrator was working in, so tell them which tenant you use. See [Billing administration](/docs/admin/billing).

## Pay by card

Card data never passes through Lahijan. Your browser sends it straight to Stripe using the publishable key from `/api/v1/billing/config`, and Lahijan only stores the card brand, last four digits and expiry.

### Save a card

1. In your own page or tool, create a Stripe PaymentMethod with Stripe.js and the publishable key. You get an id starting with `pm_`.
2. Attach it to your account:

   ```sh
   curl -X POST https://cloud.example.com/api/v1/billing/payment-methods \
     -H "Authorization: Bearer $LAHIJAN_TOKEN" \
     -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
     -H "Content-Type: application/json" \
     -d '{"stripePaymentMethodId": "pm_1234", "setDefault": true}'
   ```

List your cards with `GET /api/v1/billing/payment-methods` and remove one with `DELETE /api/v1/billing/payment-methods/{paymentMethodId}`, where the id is the Lahijan id from the list, not the `pm_` id.

### Top up your balance

1. Ask for a payment of the amount you want, in cents:

   ```sh
   curl -X POST https://cloud.example.com/api/v1/billing/topup \
     -H "Authorization: Bearer $LAHIJAN_TOKEN" \
     -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
     -H "Content-Type: application/json" \
     -d '{"amountCents": 5000, "currency": "USD"}'
   ```

   The response contains the Stripe PaymentIntent `id` and a `clientSecret`.

2. Confirm the payment with Stripe.js using the `clientSecret`.
3. When Stripe reports the payment as successful, Lahijan adds a **Top-up** credit to your ledger. This happens through Stripe's notification to Lahijan, so it can take a few seconds. A failed payment adds nothing.

## Subscription plans

A tenant administrator can publish subscription plans. A plan has a price per month or per year and an included amount. Each time Stripe reports a paid invoice for your subscription, the plan's included amount is credited to your balance.

List the active plans:

```sh
curl https://cloud.example.com/api/v1/billing/plans \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID"
```

Each plan has `name`, `interval` (`monthly` or `yearly`), `priceCents`, `includedQuotaCents` and `currency`.

Subscribe, optionally choosing one of your saved cards by its Lahijan id:

```sh
curl -X POST https://cloud.example.com/api/v1/billing/subscriptions \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"planId": "<plan id>", "paymentMethodId": "<payment method id>"}'
```

The subscription keeps the plan's price and included amount from the moment you subscribe, even if the plan changes later. Its `status` is `active`, `canceled` or `expired`.

List your subscriptions with `GET /api/v1/billing/subscriptions`. Cancel with `DELETE /api/v1/billing/subscriptions/{subscriptionId}`. By default the subscription stays active until the end of the current period; add `?cancelAtPeriodEnd=false` to cancel immediately.

## Redeem a promo code

An administrator can hand out promo codes worth a fixed amount of credit. Codes contain letters, digits and dashes.

```sh
curl -X POST https://cloud.example.com/api/v1/billing/redeem \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"code": "WELCOME-25"}'
```

On success the response is the new ledger credit (source **Top-up**, reference `promo_code:<code>`). The request fails with:

- `404` if the code does not exist in this tenant.
- `409` if it was revoked, has expired or has no uses left.

Promo codes also need card payments to be turned on, even though no card is involved.

## Disputes and refunds

If you dispute a card payment with your bank, Stripe tells Lahijan and the event is recorded, but your balance is not changed automatically in this release. Refunds are issued by an administrator as ledger credits; see [Billing administration](/docs/admin/billing).

## Who can do what

| Action                  | Permission                      | Roles that have it           |
| ----------------------- | ------------------------------- | ---------------------------- |
| Manage your cards       | `billing.payment_method.manage` | Owner, admin, member         |
| Top up by card          | `billing.payment_intent.create` | Owner, admin, member         |
| Subscribe and cancel    | `billing.subscription.manage`   | Owner, admin, member         |
| Redeem a code           | `billing.promo_code.redeem`     | Owner, admin, member         |
| List plans (admin view) | `billing.plan.read`             | Owner, admin, member, viewer |

Every payment action is recorded in the [audit log](/docs/audit/overview), for example `billing.payment_method.add`, `billing.subscription.create` and `billing.promo_code.redeem`.
