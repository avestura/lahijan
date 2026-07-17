# ADR-0013: Ledger-only billing for MVP

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan must meter usage and bill users. Options for payment processing in MVP.

Options considered:

- **Stripe integration in MVP** — real money; PCI scope; country availability
  constraints; not all deployers can use Stripe.
- **Ledger only** — admin manually tops up user balances; usage is metered and
  debited; no real money flows through the system. Ideal for OSS/self-host.
- **Both** — Stripe + admin top-up; maximum flexibility; maximum scope.
- **Skip money entirely** — track usage only; no balances; suitable for
  internal company deployments only.

## Decision

Lahijan MVP ships **ledger-only billing**:

- Admins can top up any user's balance (manual credits).
- Users consume services; usage is metered per-minute (rounded up) and debited
  from their balance.
- When a user's balance hits zero, their long-running resources (instances)
  are stopped (configurable grace period).
- Receipts/invoices (PDF) are generated for accounting.
- Prepaid credits and PAYG both flow through the same ledger.

Real payment gateway integration is **deferred to WS-27** (Phase 7).

## Consequences

- **Positive:** MVP works in every country without payment-gateway constraints.
- **Positive:** no PCI compliance scope.
- **Positive:** self-hosters can use it without wiring up Stripe.
- **Negative:** no automated revenue for SaaS deployers until WS-27.
- **Negative:** manual top-up doesn't scale beyond a few hundred users.

## Compliance

- `internal/app/lahijan/billing/` contains the price catalog, ledger, metering
  pipeline, and balance enforcement.
- Every charge is a ledger row; ledger rows are append-only (corrections via
  new rows, never UPDATE).
- Metered resources: CPU-hours, RAM-hours, disk GB-month, S3 storage, S3
  requests, DNS queries served, egress.
- Metering granularity: per minute, rounded up.

## References

- ADR-0002 (tenancy — balances belong to users in tenants)
- ADR-0008 (River — metering rollup runs as scheduled jobs)
- WS-17 (billing & metering)
- WS-27 (deferred: Stripe integration)
