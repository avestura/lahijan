# WS-27 · Payment Gateway (Stripe) + Subscriptions (DEFERRED)

```
Status: deferred
Phase: 7
Depends: WS-17 (billing & metering)
Unblocks: —
```

> **Deferred past MVP.** WS-17 ships ledger-only billing (admin top-up);
> this WS adds real money via Stripe and recurring subscriptions.

## Goal

Let users pay by card and subscribe to plans. Admins get a Stripe webhook
pipeline that reconciles with the Lahijan ledger.

## Scope (when work begins)

- Stripe integration (Stripe Go SDK)
- Payment methods: card (Stripe), prepaid codes (admin-issued)
- Plans/subscriptions (recurring): monthly plan with included quota, then
  PAYG overage
- Stripe webhook receiver → ledger entry creation
- Refund flow + dispute handling
- Invoice generation from Stripe data
- Tax: Stripe Tax integration (US/EU/UK/CA to start)
- Pricing page on marketing site wired to live catalog
- Audit + i18n throughout

## Required reading (when work begins)

- `/AGENTS.md`
- WS-17 doc (the ledger this WS extends)
- WS-19 doc (marketing pricing page)
- [Stripe Go SDK](https://github.com/stripe/stripe-go)

## Notes

- Stripe availability varies by country; for unsupported regions, the
  ledger-only mode (WS-17) remains the fallback.
- PCI scope is minimal because Stripe hosts the card input (Stripe Elements /
  Checkout); we never see card data.
