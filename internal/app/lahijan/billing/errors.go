// Package billing implements Lahijan's ledger-only billing + metering
// module (WS-17, ADR-0013). It wires the price catalog (admin-managed),
// the append-only ledger (per-user balance source of truth), the metering
// pipeline (per-minute usage collection from providers), the enforcement
// watcher (zero-balance -> instances stopped), and the receipt generator
// (per-user-per-period PDF summaries).
//
// Layering:
//
//	api/billing_handlers.go  -> billing.Service -> database (prices +
//	                                          \-> auth/audit          ledger + usage + receipts)
//	                                          \-> wasm/eventbus
//	                                          \-> billing.meter (provider pull)
//	                                          \-> billing.enforce (compute stop)
//	                                          \-> billing.receipts (PDF gen)
//
// Every privileged admin action (topup, refund, price change, force
// rebuild) calls RequirePerm via the api/middleware gate; every
// state-changing action emits an audit event before the side effect
// (status=pending) and marks the outcome after. Every ledger change also
// emits into the WASM event bus so plugins can react (low-balance
// alerts, wallet UIs, etc).
//
// All monetary amounts are INTEGER CENTIMALS (1.00 = 100). Float money
// is a hard rule from the WS-17 DoD; we never convert to float anywhere
// in the package.
//
// Per pillar 1, this package is named "billing" — never "stripe",
// "payment", or "invoice" in user-facing copy. End users see "balance",
// "ledger", "receipt".
package billing

import "errors"

// ErrPriceNotFound is returned when the requested price row does not
// exist within the caller's tenant. The handler maps it to 404.
var ErrPriceNotFound = errors.New("billing: price not found")

// ErrLedgerEntryNotFound is returned when the requested ledger row does
// not exist within the caller's tenant. The handler maps it to 404.
var ErrLedgerEntryNotFound = errors.New("billing: ledger entry not found")

// ErrUsageEventNotFound is returned when the requested usage row does
// not exist within the caller's tenant. The handler maps it to 404.
var ErrUsageEventNotFound = errors.New("billing: usage event not found")

// ErrReceiptNotFound is returned when the requested receipt does not
// exist within the caller's tenant. The handler maps it to 404.
var ErrReceiptNotFound = errors.New("billing: receipt not found")

// ErrUserNotFound is returned when the supplied user does not exist
// (admin operations target a specific user_id). The handler maps it to 404.
var ErrUserNotFound = errors.New("billing: user not found")

// ErrInvalidAmount is returned when the caller passes a negative amount
// (or zero where a positive amount is required) to a topup / refund /
// charge path.
var ErrInvalidAmount = errors.New("billing: amount must be a positive integer")

// ErrInvalidPeriod is returned when the supplied period is malformed
// (period_start >= period_end, period spans more than a year, etc).
var ErrInvalidPeriod = errors.New("billing: period is malformed")

// ErrInvalidResource is returned when the supplied resource_type /
// unit pair is empty or has whitespace.
var ErrInvalidResource = errors.New("billing: resource_type and unit are required")

// ErrInvalidCurrency is returned when the supplied currency code is not
// a 3-letter ISO 4217 code. (Multi-currency is Phase 7, but the shape
// is enforced from day 1 so the migration is additive.)
var ErrInvalidCurrency = errors.New("billing: currency must be a 3-letter ISO 4217 code")

// ErrDuplicateIdempotencyKey is returned when a metering insert hits a
// duplicate idempotency_key. Callers MUST treat this as "row already
// exists" and skip the insert (the previous run already recorded it).
var ErrDuplicateIdempotencyKey = errors.New("billing: idempotency key already exists")

// ErrInsufficientBalance is returned when a charge would push the user's
// balance below the configured floor (typically 0; the enforcement job
// handles the grace period separately). The handler maps it to 402.
var ErrInsufficientBalance = errors.New("billing: insufficient balance")
