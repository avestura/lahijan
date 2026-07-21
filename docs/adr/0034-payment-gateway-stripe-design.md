# ADR-0034: Payment Gateway (Stripe) design — thin HTTP client, ledger-layered, webhook-first

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer
- **Supersedes:** none (extends ADR-0013)

## Context

WS-27 ("Payment Gateway (Stripe) + Subscriptions") lifts ADR-0013's
"ledger-only for MVP" cap by letting end users pay by card and subscribe
to recurring plans. ADR-0013 always anticipated this WS — it explicitly
deferred the gateway to Phase 7 and shipped the ledger as the single
source of truth for balance so the future gateway could land additively.

The architectural forces at play:

1. **PCI scope must stay minimal.** The Lahijan binary must never see
   card data. Stripe Checkout / Payment Element hosts the card input;
   Lahijan only ever sees the resulting PaymentMethod / PaymentIntent
   id. This is the same posture ADR-0013 already committed to.

2. **The Stripe Go SDK is large + invasive.** `github.com/stripe/stripe-go/v76`
   pulls in a generated type tree spanning ~thousands of types and
   does its own `init()`-time state mutation (process-wide API key).
   That does not match Lahijan's "providers/ → pure driver" layering,
   it makes httptest-backed tests painful (the SDK bootstraps its own
   `http.Client` and base URL via package-level state), and it adds a
   >10 MB dependency tree to the binary. The three Phase-3 providers
   (Incus / PowerDNS / SeaweedFS) already chose thin internal HTTP
   clients for the same reasons (ADR-0025, ADR-0026, ADR-0027).

3. **Webhooks are the source of truth for "did the money land".**
   Stripe's own docs mandate listening to webhooks rather than
   trusting the synchronous API response. Idempotency is mandatory
   (Stripe retries webhook delivery) and a missed event means a
   missed ledger credit.

4. **The ledger is immutable (append-only by trigger, migration
   0035).** The gateway must reconcile to ledger entries, never write
   to the ledger directly; refunds + disputes are NEW ledger rows.

5. **Operator-deployment flexibility.** Self-hosters may not have a
   Stripe account (per ADR-0013's "ledger-only mode is the fallback
   for unsupported regions"). The gateway MUST be opt-in via config,
   not a hard dependency.

Options considered:

- **Option A — Stripe Go SDK + Stripe-hosted Checkout.** Pros:
  typed end-to-end, official surface. Cons: huge dependency, doesn't
  fit the providers/* pattern, package-level state fights httptest
  fakes, opinionated retry behaviour we don't control. PCI posture
  is the same with the thin client because card input is hosted
  either way.
- **Option B — Thin internal HTTP client over the Stripe REST API +
  Stripe-hosted Payment Element / Checkout.** Pros: pattern-uniform
  with WS-11/12/13; trivially httptest-able; zero new top-level deps
  in `go.mod`; full control over retry/tracing/redaction; matches the
  "providers/ → pure driver" rule. Cons: we own the request/response
  types (mitigation: Stripe's REST API is documented and stable).
- **Option C — Embed Stripe Elements server-side and tokenize in
  Lahijan.** Rejected immediately: pulls card data into Lahijan's
  blast radius and forfeits the SaaS-tier PCI posture ADR-0013
  committed to.

## Decision

WS-27 implements the Stripe gateway as a thin internal HTTP client
under `internal/app/lahijan/providers/stripe/`, layered on top of the
WS-17 ledger via a new `billing/payments` service. The package
implements `providers.Provider` (`Name / Ping / Capabilities`) for
consistency with the other three drivers but is **opt-in**:
unconfigured, it stays nil and every payment endpoint degrades to
501, exactly like the Incus / PowerDNS / SeaweedFS providers do when
their config is absent.

Concretely:

- **Provider driver** (`internal/app/lahijan/providers/stripe/`):
  - Talks HTTPS to `https://api.stripe.com/v1/*` using a standard
    `net/http.Client` configured for the Stripe account's region
    (configurable base URL so tests point at `httptest`). The
    `Bearer <secret_key>` header is sent on every request; the
    secret is AES-GCM-encrypted at rest in operator config (reuses
    the existing `auth/secrets.Crypto` envelope).
  - Owns minimal JSON request/response types in `types.go` covering
    only the surfaces Lahijan uses: PaymentIntent create/capture,
    SetupIntent, PaymentMethod attach, Customer create/attach,
    Subscription create/list/cancel, Invoice pay, Webhook event
    listing.
  - HMAC-SHA256 webhook signature verification
    (`Stripe-Signature` header, `t=...,v1=...` format) lives in
    `webhooks.go`. The signed-payload check runs before any DB write.
  - Adds **zero** new runtime dependencies: the package imports only
    stdlib (`net/http`, `crypto/hmac`, `encoding/json`,
    `crypto/sha256`) plus `go.opentelemetry.io/otel` for per-call
    tracing (already required by ADR-0016).

- **Persistence** (`internal/app/lahijan/database/`):
  - Five new tenant-scoped tables (migrations `0041_billing_plans`,
    `0042_billing_payment_methods`, `0043_billing_subscriptions`,
    `0044_billing_promo_codes`, `0045_billing_webhook_events`).
  - All carry the standard `id UUID PK`, `tenant_id UUID NOT NULL`,
    `created_at`, `updated_at` envelope per ADR-0002. The
    `stripe_customer_id` column on `payment_methods` is
    AES-GCM-encrypted (the column is `TEXT`; the envelope is the
    same `base64(nonce||ct||tag)` shape the auth subsystem uses).
  - `webhook_events` is append-only: a UNIQUE on `stripe_event_id`
    is the idempotency boundary (duplicate Stripe delivery =
    `ON CONFLICT DO NOTHING` -> return 200 OK without re-applying
    the effect).
  - `promo_codes` carry an optional `applies_to_plan_id`, a
    `credit_cents` (the amount credited on redeem), an optional
    `expires_at`, and a `max_uses` / `times_used` pair so the
    admin can issue single-use codes.

- **Service layer** (`internal/app/lahijan/billing/`):
  - `payments.go` — the entrypoint for `POST /api/v1/billing/topup`
    (synchronous: create PaymentIntent, return the client secret
    so the SPA can confirm via Stripe.js). On `payment_intent.succeeded`
    webhook the service appends a `source=stripe_payment` ledger
    credit row keyed by the event id (idempotent).
  - `subscriptions.go` — `POST /api/v1/billing/subscriptions`
    creates the Stripe Subscription; the webhook handler credits
    the included-quota row when `invoice.paid` lands. Cancellation
    flips the row to `canceled` and lets the Stripe-driven expiry
    take effect.
  - `promo_codes.go` — admin CRUD + user `POST
    /api/v1/billing/redeem`. Redeem credits the user's balance
    (a normal `source=topup` ledger row with reference
    `promo_code:<code>`); a UNIQUE on `(tenant_id, code)` + an
    atomic `times_used = times_used + 1` guard the single-use case.
  - `webhooks.go` — verifies the signature, deduplicates against
    `webhook_events`, dispatches per-event-type handlers. Every
    ledger effect has an idempotency key of
    `stripe:<event_id>:<effect>`.

- **HTTP surface**: a new `/api/v1/billing/*` user tree and
  `/api/v1/admin/billing/{plans,promo_codes}/*` admin tree. The
  webhook receiver `POST /api/v1/webhooks/stripe` is the ONE
  endpoint with no `RequirePerm` and no tenant scope; it parses
  the raw body before the JSON middleware so the signature is
  computed on the exact bytes Stripe sent. The audit gate treats
  it specially (no rbac check; signature is the auth).

- **Operator config** (`internal/app/lahijan/conf/`):
  - `billing.stripe.enabled` (bool, default false).
  - `billing.stripe.secret_key` (string, AES-GCM-encrypted at
    rest in env). Required when `enabled=true`.
  - `billing.stripe.publishable_key` (string, exposed to the SPA
    so Stripe.js can bootstrap).
  - `billing.stripe.webhook_secret` (string, the `whsec_...` used
    to verify signatures).
  - `billing.stripe.api_base_url` (string, default
    `https://api.stripe.com`; overridable for tests).
  - When `enabled=false` (the default), the gateway stays nil,
  the routes return 501, and the rest of the billing subsystem
  keeps operating in ADR-0013's ledger-only mode.

- **Audit + i18n**: every privileged action
  (`billing.plan.create`, `billing.plan.update`,
  `billing.promo_code.create`, `billing.subscription.cancel` for
  admins, etc.) emits an audit row pre + post via the standard
  `auditEmit` / `auditMarkOutcome` helpers. Every user-facing
  string lands in both en + fa. WASM event-bus topics
  (`billing.payment.succeeded`, `billing.subscription.activated`,
  `billing.promo_code.redeemed`) are registered in the canonical
  event registry.

## Consequences

- **Positive:** zero transitive bloat — `go.mod` does not grow any
  new module for the Stripe driver; every WS-27 test is a pure
  httptest round-trip.
- **Positive:** ADR-0013's ledger-only deployments keep working
  unchanged (gateway is opt-in).
- **Positive:** PCI posture is unchanged — Lahijan still never sees
  card data; the SPA confirms PaymentIntents via Stripe.js using a
  client secret, and the gateway only ever handles PaymentMethod /
  PaymentIntent ids.
- **Positive:** pattern-uniform with the three Phase-3 providers —
  a maintainer reading any driver sees the same shape (client +
  types + per-area files + `fake/`).
- **Negative:** we own the JSON types. If Stripe adds a field we
  want, we add it ourselves. Mitigation: Stripe's REST API is
  documented and version-pinned via the `Stripe-Version` header
  (set once at client construction).
- **Negative:** webhook receiver is the source of truth for
  payment success, which means a down Lahijan instance can miss
  events. Mitigation: Stripe retries delivery for ~3 days; the
  `webhook_events` UNIQUE constraint absorbs duplicates. A
  reconciliation job that pulls recent events via the Events API
  is a follow-up WS (documented in WS-27's "deferred" list).

## Compliance

- `internal/app/lahijan/providers/stripe/client.go` is the single
  HTTP entry point; every area file (`payment_intents.go`,
  `subscriptions.go`, `webhooks.go`, ...) calls through it. The
  driver imports only stdlib + `go.opentelemetry.io/otel`.
- `internal/app/lahijan/providers/stripe/fake/server.go` is the
  httptest fake every test in the package reuses; it implements
  the same in-memory PaymentIntent / Customer / Subscription
  graph the real Stripe API does.
- The webhook receiver at `POST /api/v1/webhooks/stripe` is
  registered outside the rbac gate in `api/router.go`; it parses
  the raw body before JSON middleware and verifies the signature
  via `stripe.VerifyWebhook` in the driver.
- Every ledger effect originating from the gateway carries an
  idempotency key of the shape `stripe:<event_id>:<effect>` and a
  `source` of `stripe_payment` (topup), `stripe_refund`
  (refund), or `stripe_subscription` (plan credit). The
  append-only ledger dedupe + the `webhook_events` UNIQUE
  together guarantee at-most-one application per Stripe event.
- `go.mod` MUST NOT grow a `stripe/stripe-go` entry for this WS;
  if a future WS needs the SDK it should write a new ADR
  superseding this one.

## Deferred (documented in WS-27)

The following WS-27 scope items are explicitly **out of scope** for
this ADR / this implementation pass; follow-up WSs will land them:

- **Stripe Tax** integration (per-country tax matrix). The gateway
  stores tax-related fields verbatim if Stripe sends them; the
  Lahijan-side tax calculation is deferred.
- **Receipt / invoice generation from Stripe data.** WS-17's
  receipt generator already exists; mapping Stripe invoice ids to
  Lahijan receipts is a follow-up.
- **Dispute flow as a first-class UI.** `charge.dispute.created`
  webhook events are accepted and recorded in
  `webhook_events` (with a `billing.charge.disputed` ledger row);
  a dedicated admin panel for disputes is deferred.
- **Marketing pricing page wired to live catalog.** The catalog
  is tenant-scoped; a public catalog endpoint is a follow-up.
- **Stripe Events API reconciliation sweep.** A periodic River job
  that pulls recent events to cover the "Lahijan was down" window
  is the safety net on top of Stripe's own retries; deferred.

## References

- ADR-0013 (ledger-only billing — the foundation this WS extends)
- ADR-0002 (multi-tenant row-level isolation)
- ADR-0025 (Incus client library — the pattern this ADR mirrors)
- ADR-0026 (PowerDNS client library — sister decision)
- ADR-0027 (SeaweedFS client library — sister decision)
- WS-17 (billing & metering — the ledger layer)
- WS-27 (payment gateway + subscriptions — this WS)
- [Stripe webhooks docs](https://docs.stripe.com/webhooks)
- [Stripe Payment Intents API](https://docs.stripe.com/api/payment_intents)
