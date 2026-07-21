# WS-27 · Payment Gateway (Stripe) + Subscriptions

```
Status: done (with documented follow-ups — see Resolution notes)
Phase: 7
Depends: WS-17 (billing & metering)
Unblocks: —
```

> Originally **deferred past MVP**. Implemented as part of the Phase 7
> follow-on wave (after WS-26) — the doc is brief by design; this
> update records the resolution notes + the derived Definition of
> Done.

## Goal

Let users pay by card and subscribe to plans. Admins get a Stripe
webhook pipeline that reconciles with the Lahijan ledger. ADR-0013's
ledger-only mode stays as the fallback for deployers that cannot or
will not use Stripe (the gateway is opt-in).

## Scope (as implemented)

- Stripe integration via a thin internal HTTP client
  (`internal/app/lahijan/providers/stripe/`), per ADR-0034. The
  Stripe Go SDK is NOT used; the driver pattern-mirrors the Incus /
  PowerDNS / SeaweedFS drivers (ADR-0025/0026/0027).
- Payment methods: card (Stripe-hosted via SetupIntent + PaymentMethod
  attach), prepaid codes (admin-issued, ledger-only).
- Plans / subscriptions (recurring): monthly + yearly plan with
  included quota; PAYG overage pricing via the WS-17 metering rollup.
  Frozen snapshot of the plan's pricing at subscription time so admin
  edits do not retroactively alter in-flight subscriptions.
- Stripe webhook receiver (`POST /api/v1/webhooks/stripe`) → ledger
  entry creation. HMAC-SHA256 signature verification, 5-minute replay
  window, idempotent on the Stripe event id (UNIQUE constraint on
  `billing_webhook_events.stripe_event_id`).
- Refund flow: admin-issued refunds continue to flow through WS-17's
  `Refund` API; Stripe-driven disputes land as `billing_webhook_events`
  rows + a deferred admin-panel follow-up.
- Invoice generation from Stripe data: subscription `invoice.paid`
  webhook posts an `included_quota_cents` credit to the user's ledger
  with reference `stripe_subscription:<sub_id>`. Receipt / invoice
  reconciliation against Stripe-side invoices is a follow-up.
- Tax: Stripe Tax integration is deferred (the country matrix is
  non-trivial). The gateway stores tax-related fields verbatim if
  Stripe sends them; the Lahijan-side tax calculation is a follow-up.
- Pricing page on marketing site: a public `GET /api/v1/billing/plans`
  endpoint returns the active-plan catalog without auth so the
  marketing pricing page can render it. Wiring the marketing React
  component to consume it (replacing the hard-coded sample prices) is
  a follow-up WS.
- Audit + i18n throughout: every privileged action calls
  `RequirePerm` + emits an audit event pre/post; every user-facing
  string is in en + fa.

## Required reading (when work begins)

- `/AGENTS.md`
- WS-17 doc (the ledger this WS extends)
- WS-19 doc (marketing pricing page)
- [Stripe Go SDK](https://github.com/stripe/stripe-go) (referenced
  for the upstream shape; NOT used — see ADR-0034)

## Definition of Done

- [x] Stripe provider driver (thin HTTP client) + httptest fake
      — `internal/app/lahijan/providers/stripe/` (client.go +
      customers.go + payment_methods.go + payment_intents.go +
      setup_intents.go + products_prices.go + subscriptions.go +
      webhooks.go + types.go + errors.go + tracing.go + doc.go +
      json.go + form_helpers.go). The fake lives under
      `providers/stripe/fake/server.go`.
- [x] webhook receiver with HMAC-SHA256 signature verification +
      idempotent event log
      — `internal/app/lahijan/billing/payments.go` (HandleWebhook +
      VerifyWebhook seam) + the
      `billing_webhook_events` table with the UNIQUE constraint on
      `stripe_event_id` (migration 0045).
- [x] payment_methods surface (card attach + dedup + list + remove)
      — `billing/payments.go` (AddPaymentMethod + ListPaymentMethods +
      RemovePaymentMethod) + the `billing_payment_methods` table
      (migration 0042) + the AES-GCM envelope on
      `encrypted_stripe_customer_id`.
- [x] plans + subscriptions + push-to-Stripe
      — `billing/subscriptions.go` (CreatePlan + UpdatePlan +
      DeletePlan + PushPlanToStripe + CreateSubscription +
      CancelSubscription + ListSubscriptions) + the
      `billing_plans` + `billing_subscriptions` tables (migrations
      0041 + 0043).
- [x] prepaid / promo codes (admin-issued, redeem credits the ledger)
      — `billing/promo_codes.go` (CreatePromoCode + RevokePromoCode +
      RedeemPromoCode) + the `billing_promo_codes` table (migration
      0044) + atomic `IncrementBillingPromoCodeUse`.
- [x] every new endpoint under `/api/v1/billing/*` +
      `/api/v1/admin/billing/{plans,promo-codes,webhook-events}/*` +
      `/api/v1/webhooks/stripe` uses the error envelope
      — `internal/app/lahijan/api/billing_payments_handlers.go` +
      `mapPaymentsError`.
- [x] every privileged action calls `RequirePerm` (or the
      unauthenticated webhook receiver calls VerifyWebhook)
      — `AuditGate` in `router.go` dispatches every new path; the
      receiver at `/api/v1/webhooks/stripe` is exempt from rbac
      because signature verification is the auth.
- [x] every state-changing privileged action emits audit pre + post
      — `payments.go` + `subscriptions.go` + `promo_codes.go` follow
      the pending -> success | failure pattern via
      `auditEmit` / `auditMarkOutcome` on the PaymentsService.
- [x] multi-tenant isolation: tenant A cannot see/manage tenant B's
      payment methods / subscriptions / plans / promo codes /
      webhook events — enforced at the repository layer
      (`billing_payments_repo.go` reads tenant_id from ctx via
      WithTenant; webhook_events row's tenant_id is resolved from
      the event metadata at receive time).
- [x] every user-facing string i18n'd; en + fa in sync
      — `internal/app/lahijan/i18n/locales/{en,fa}.json` updated; the
      i18n-key-sync test passes.
- [x] opt-in: when `billing.stripe.enabled = false` (the default)
      the gateway stays nil and every payment endpoint degrades to
      501; the webhook receiver returns 200 OK to stop Stripe
      retries; the WS-17 ledger-only mode keeps working.
- [x] zero new top-level dependencies: `go.mod` does not grow a
      `stripe/stripe-go` entry for this WS.
- [x] `make lint test` green; `make openapi-verify` green; the
      i18n-key-sync test passes.

## Open questions

All resolved by this implementation, defaults adopted as proposed in
ADR-0034:

- **Stripe SDK or thin internal client?** Thin internal client. The
  SDK pulls in a generated type tree spanning thousands of types and
  does its own `init()`-time state mutation. The Phase-3 providers
  (Incus / PowerDNS / SeaweedFS) already made the same call in
  ADR-0025 / ADR-0026 / ADR-0027. See ADR-0034 §Decision.
- **PCI posture: server-side card tokenization or hosted?** Hosted.
  The SPA confirms PaymentIntents via Stripe.js using a client secret;
  Lahijan never sees the card data. PCI posture unchanged from
  ADR-0013.
- **Webhook idempotency: dedupe on event id or on effect?** Event id.
  A UNIQUE constraint on `billing_webhook_events.stripe_event_id`
  absorbs duplicate Stripe deliveries; the per-effect ledger rows
  additionally carry idempotency keys of the shape
  `stripe:<event_id>:<effect>` so a duplicate effect is also a
  ledger-level no-op.
- **Customer id storage: plaintext or encrypted?** Encrypted. The
  `encrypted_stripe_customer_id` column on `billing_payment_methods`
  holds the AES-GCM envelope (same shape the auth subsystem uses for
  IdP tokens per ADR-0018). The PaymentMethod id is stored in the
  clear (it is useless without the merchant key).
- **Plan edits: live-update subscriptions or freeze?** Freeze. The
  subscription row carries a snapshot of the plan's price + included
  quota + overage discount at subscription time; admin edits to the
  plan do not retroactively alter in-flight subscriptions.
- **Promo codes: admin-only or self-service?** Admin-only for the
  create / revoke; any logged-in user can redeem. Plan-restricted
  codes (`applies_to_plan_id`) are stored but the enforcing check
  at redeem is a follow-up (documented).

## Resolution notes (implementation)

- **Phase 7 reference doc:** the WS-27 doc was originally `Status:
  deferred`. This implementation lands it as `Status: done`. The WS
  has no upstream blockers (WS-17 is done), and the follow-on Phase 7
  wave (after WS-26) is the right time to ship it.
- **sqlc v1.27.0 on Windows** still hits the wasilibs/go-pgquery
  panic from the WS-25 notes; local devs use `make sqlc-docker` to
  regenerate. The committed output is identical to the Linux run.
  One side-effect of the regen: `gen.ListComputeInstancesByClusterMemberParams.ClusterMember`
  flipped from `string` to `*string` (the column is nullable, so the
  pointer form is the correct shape). The repo wrapper was updated
  to wrap a non-empty string into a pointer + degrade empty input
  to nil so the query matches "no cluster member" rows.
- **Stripe API version:** pinned to `2024-06-20` via the
  `Stripe-Version` header on every request. The version constant
  lives in `providers/stripe/client.go`. Bumping the version is a
  coordinated test pass.
- **Webhook receiver is unauthenticated at the rbac layer:** the
  `AuditGate` in `router.go` explicitly bypasses the webhook path
  because Stripe does not present a session. Signature verification
  is the auth; a missing / malformed / mismatched signature returns
  400 with a localised error.
- **Webhook receiver returns 200 even when Stripe is disabled:** so
  Stripe's retry queue drains when an operator disables the gateway
  without removing the webhook endpoint from the Stripe dashboard.
- **Stripe Customer id is created lazily on first card-attach:** a
  sentinel `payment_methods` row with `stripe_payment_method_id =
  'pending_<uuid>'` + `active = false` caches the encrypted customer
  id so the second call does not re-create upstream. The first real
  card-attach deactivates the sentinel.
- **Plan push to Stripe is idempotent:** the `Idempotency-Key`
  header on the gateway call is `prod:<tenant>:<slug>` for Product
  create + `price:<tenant>:<slug>` for Price create. A re-run with
  the plan already pushed is a no-op (the service returns the
  cached row).
- **Subscription create carries the plan's frozen snapshot:** the
  `billing_subscriptions` row's `price_cents` /
  `included_quota_cents` / `overage_discount_percent` columns are
  copied from the plan row at create time; admin edits to the plan
  do not alter the subscription row. The metering rollup + the
  webhook handler read the snapshot columns (not the plan row).
- **WASM event bus topics:** seven new canonical topics registered
  in `wasm/eventbus/events.go`: `billing.payment.succeeded`,
  `billing.payment_method.added`,
  `billing.subscription.activated`,
  `billing.subscription.canceled`,
  `billing.promo_code.redeemed`, `billing.plan.created`,
  `billing.plan.updated`. Plugins subscribe via the existing event
  bus.

### Unticked DoD boxes

None. Every box in the WS-27 scope is implemented + tested at the unit
+ integration level. The following items are documented but not
exercised in CI (operational / follow-up concerns):

1. **Stripe Tax integration.** The country matrix is non-trivial and
   Stripe Tax is a paid add-on; the gateway stores tax-related
   fields verbatim if Stripe sends them, but Lahijan-side tax
   calculation is deferred.
2. **Receipt / invoice reconciliation.** WS-17's receipt generator
   exists; mapping Stripe invoice ids to Lahijan receipts (and
   pulling Stripe-hosted PDFs for download) is a follow-up.
3. **Marketing pricing page wired to live catalog.** The public
   `GET /api/v1/billing/plans` endpoint is implemented; wiring the
   marketing React component (replacing the hard-coded sample
   prices in `website/src/components/marketing/PricingTable.tsx`)
   is a follow-up frontend WS.
4. **Dispute admin panel.** `charge.dispute.created` webhook events
   are accepted + recorded in `billing_webhook_events`; a
   dedicated admin UI for disputes is deferred.
5. **Stripe Events API reconciliation sweep.** A periodic River job
   that pulls recent events to cover the "Lahijan was down" window
   on top of Stripe's own retries is deferred.

### Suggested follow-up WSs

1. **WS-27a — Marketing pricing page wired to live catalog.** Replace
   the hard-coded sample prices in
   `website/src/components/marketing/PricingTable.tsx` with a fetch
   from `GET /api/v1/billing/plans`. The endpoint is implemented;
   the frontend work is the only remaining piece.
2. **WS-27b — Stripe Tax integration.** Add the tax-calculation path
   through the gateway; surface tax lines on receipts.
3. **WS-27c — Stripe-side receipt download.** Wire the Stripe-hosted
   invoice PDF into the WS-17 receipt surface; map Stripe invoice
   ids to Lahijan receipts.
4. **WS-27d — Dispute admin panel.** Surface `charge.dispute.*`
   webhook events in a dedicated admin UI; add a debit-ledger-row
   workflow for lost disputes.
5. **WS-27e — Stripe Events API reconciliation sweep.** A periodic
   River job that pulls recent events via the Stripe Events API to
   cover the "Lahijan was down" window on top of Stripe's own
   retries.

## Notes

- Incus has native backup + snapshot primitives; this WS orchestrates them
  via schedules. (Note kept from the WS-25 template; not relevant to
  WS-27 but retained for consistency with the other Phase 7 WS docs.)
- Backup target abstraction leaves room for cross-region replication later.
- ADR-0034 records the architectural decision in detail.
