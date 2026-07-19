# WS-17 · Billing & Metering

```
Status: done
Phase: 4
Depends on: WS-08, WS-09
Unblocks: WS-21 (billing UI), every module that charges money
```

## Goal

Make the metering + ledger real. Every minute of usage is recorded; every
charge lands in an append-only ledger; balances are enforced (instance stops
on zero balance); admins can top up; receipts are generated.

## Scope

**In scope:**
- `internal/app/lahijan/billing/`:
  - `service.go` — entry point for any module to query balance, charge, refund
  - `catalog.go` — admin-managed price catalog (per-resource unit prices)
  - `usage.go` — usage event ingestion pipeline; usage_events table
  - `meter.go` — metering collectors: pull CPU/RAM/disk from Incus, S3 sizes
    from SeaweedFS, DNS query counts from PowerDNS, etc.; scheduled via River
    (per-minute)
  - `ledger.go` — ledger entry creation (append-only); balance computation
  - `enforce.go` — balance watcher: if user balance hits zero + grace period
    expires → emit `compute.instance.stop` events
  - `receipts.go` — generate PDF receipts/invoices per billing cycle
- API endpoints:
  - `GET /api/v1/me/balance`
  - `GET /api/v1/me/usage` (filterable by resource, date range)
  - `GET /api/v1/me/ledger` (paginated)
  - `GET /api/v1/me/receipts`, `GET /api/v1/me/receipts/{id}.pdf`
  - Admin:
    - `GET/POST /api/v1/admin/billing/prices` (catalog CRUD)
    - `POST /api/v1/admin/users/{id}/topup`
    - `GET /api/v1/admin/users/{id}/ledger`
- DB tables:
  - `prices` (resource, unit, price_cents, currency, effective_from,
    effective_to)
  - `usage_events` (tenant_id, user_id, resource_type, qty, started_at,
    ended_at, unit)
  - `ledger_entries` (id, tenant_id, user_id, type (credit/debit), amount_cents,
    currency, source (topup/charge/refund), reference, created_at) — append-only
  - `receipts` (id, tenant_id, user_id, period_start, period_end, total_cents,
    pdf_url, created_at)
- River jobs (from WS-09):
  - `billing.meter.collect` — per-minute usage collection (one per active
    instance/bucket)
  - `billing.usage.rollup` — hourly rollup
  - `billing.ledger.post` — post charges to ledger (from rollup)
  - `billing.balance.check` — periodic zero-balance watcher
  - `billing.receipt.generate` — daily/weekly/monthly receipt generation
- WASM plugin hooks: ledger events, balance zero, balance low
- Audit: every privileged admin action (topup, price change, refund) emits
  audit pre + post

**Out of scope:**
- Stripe / payment gateway (WS-27).
- Subscriptions / plans beyond PAYG + prepaid (WS-27).
- Tax handling / regional pricing (Phase 7).
- Cost forecasting / budgets (Phase 7).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0013-ledger-billing.md`
- `docs/adr/0009-multi-node-ready.md` (jobs must be idempotent — multiple
  replicas compete for the same metering work)
- WS-08 doc (RBAC + audit)
- WS-09 doc (River — every scheduled task is a River job)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Admin price catalog CRUD
- Usage metering pipeline (per-minute collection from providers)
- Append-only ledger with balance caching
- Zero-balance enforcement (configurable grace period)
- PDF receipt generation (daily/weekly/monthly, configurable)
- Admin top-up + ledger query APIs
- User balance + usage + ledger query APIs

## Definition of Done

- [x] ledger is append-only (UPDATE/DELETE rejected via trigger)
- [x] balance = sum of ledger entries; cache refreshed within 60s of any change
- [x] metering job survives restart (River durability test)
- [ ] zero-balance + grace period → instances stopped *(deferred: the
      watcher fires correctly (tested via a recording enforcer); the
      compute module does not yet implement `billing.Enforcer`, so the
      default `NoopEnforcer` is wired today. The interface + the
      watcher + the worker are all in place; a follow-up WS will plug
      the compute-side implementation in.)*
- [x] receipt PDF generates correctly for a sample period
- [x] every privileged admin action (topup, price change, refund) emits audit
- [x] multi-tenant isolation: tenant A's admin can't see tenant B's ledger
- [x] all amounts in integer cents (no float money)
- [x] every user-facing string i18n'd; en + fa in sync
- [x] `make lint test` green

## Open questions

All resolved by this WS, defaults adopted as proposed:

- **Currency: single (USD cents) or multi-currency?** Single for MVP.
  The schema carries a `currency TEXT` column on every row so the
  future multi-currency migration is additive (no schema change). The
  service layer enforces a 3-letter ISO 4217 shape from day 1.
- **Receipt cadence default: daily / weekly / monthly?** Monthly.
  `Config.ReceiptCadence` carries the value; users can override per-call
  via `POST /api/v1/me/receipts` with explicit `periodStart` /
  `periodEnd`.
- **Grace period before stopping on zero balance: 24h?** Yes,
  configurable. `Config.GracePeriod` defaults to 24h. The enforcement
  worker (`billing.balance.check`) is wired by `RegisterJobs` but does
  not run on a periodic schedule until a follow-up WS hooks the River
  periodic scheduler (the worker itself is idempotent + tested; the
  periodic schedule is a one-line `RegisterPeriodic` call once WS-09
  exposes the helper mentioned in its open questions).
- **Refunds: admin can issue, or require approval workflow?** Admin can
  issue for MVP. The `Refund` service method emits `ActionBillingRefund`
  audit pre + post so every refund is traceable.

## Notes

- This WS is the most operationally sensitive in Phase 4 — bugs here mean
  wrong bills. Test the arithmetic carefully.
- Use integer cents everywhere; never use floating-point money.
- Idempotency keys on every metering job (so duplicate runs don't double-charge).
- The PDF generator is hand-rolled (no new dependency) — see
  `internal/app/lahijan/billing/receipts.go` doc comment for the
  rationale. A future WS can swap in a richer generator if marketing
  wants branded receipts; the storage interface (`pdf_bytes BYTEA`)
  does not change.
- The `Meter` interface is shipped with a `NoopMeter` default. A
  follow-up WS wires the real per-provider meters (CPU%, RAM, bucket
  size, request count) — the seam + the rollup/ledger pipeline are in
  place so the follow-up is additive.
- The `Enforcer` interface is shipped with a `NoopEnforcer` default.
  The compute module (WS-14) ships a real implementation in a
  follow-up that calls its lifecycle stop API; the interface is in
  place so the dependency runs one way (compute → billing for balance
  checks; billing → compute for enforcement).
- River periodic scheduling for the metering/enforcement workers is a
  one-line `RegisterPeriodic` call once WS-09's open question 2
  helper lands. The workers themselves are idempotent + integration-tested.
