# WS-17 · Billing & Metering

```
Status: pending
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

- [ ] ledger is append-only (UPDATE/DELETE rejected via trigger)
- [ ] balance = sum of ledger entries; cache refreshed within 60s of any change
- [ ] metering job survives restart (River durability test)
- [ ] zero-balance + grace period → instances stopped
- [ ] receipt PDF generates correctly for a sample period
- [ ] every privileged admin action (topup, price change, refund) emits audit
- [ ] multi-tenant isolation: tenant A's admin can't see tenant B's ledger
- [ ] all amounts in integer cents (no float money)
- [ ] every user-facing string i18n'd; en + fa in sync
- [ ] `make lint test` green

## Open questions

- Currency: single (USD cents) or multi-currency? (Default: single for MVP;
  multi-currency is Phase 7.)
- Receipt cadence default: daily / weekly / monthly? (Default: monthly; user
  can override.)
- Grace period before stopping on zero balance: 24h? (Default: yes,
  configurable.)
- Refunds: admin can issue, or require approval workflow? (Default: admin can
  issue for MVP; approval workflow is Phase 7.)

## Notes

- This WS is the most operationally sensitive in Phase 4 — bugs here mean
  wrong bills. Test the arithmetic carefully.
- Use integer cents everywhere; never use floating-point money.
- Idempotency keys on every metering job (so duplicate runs don't double-charge).
