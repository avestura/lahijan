-- Billing queries (WS-17, ADR-0013). Five tables, each tenant-scoped via
-- WithTenant at the repository seam EXCEPT the price catalog's admin
-- cross-tenant helpers (GetGlobalByID etc.) which are gated by
-- RequirePerm("billing.price_catalog.update") at the service layer.
--
-- The ledger_entries + usage_events tables are append-only at the DB level
-- (trigger in migration 0035 / 0036); these query files therefore expose
-- only INSERT and SELECT on either. user_balances is mutable (it's a
-- cache) and receipts is mutable (status + pdf_bytes flip).

-- ===========================================================================
-- prices: admin-managed price catalog.
-- ===========================================================================

-- name: CreatePrice :one
--: tenant-scoped
INSERT INTO prices (
    tenant_id, resource_type, unit, price_cents, currency,
    effective_from, effective_to
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPriceByID :one
--: tenant-scoped
SELECT * FROM prices
WHERE tenant_id = $1 AND id = $2;

-- name: GetCurrentPrice :one
--: tenant-scoped; returns the one "effective_to IS NULL" row for the
--: (tenant, resource, unit) tuple, or no rows if none is currently in effect.
SELECT * FROM prices
WHERE tenant_id = $1
  AND resource_type = $2
  AND unit = $3
  AND effective_to IS NULL;

-- name: GetPriceAt :one
--: tenant-scoped; returns the price row effective for the given timestamp.
--: Used by the rollup job to pick the right price for a historical minute.
SELECT * FROM prices
WHERE tenant_id = $1
  AND resource_type = $2
  AND unit = $3
  AND effective_from <= $4
  AND (effective_to IS NULL OR effective_to >= $4)
ORDER BY effective_from DESC
LIMIT 1;

-- name: ListPrices :many
--: tenant-scoped; ordered by resource_type then unit so the catalog reads
--: as a stable grouped list.
SELECT * FROM prices
WHERE tenant_id = $1
ORDER BY resource_type ASC, unit ASC, effective_from DESC
LIMIT $2 OFFSET $3;

-- name: CountPrices :one
--: tenant-scoped.
SELECT count(*) FROM prices WHERE tenant_id = $1;

-- name: ExpireCurrentPrice :exec
--: tenant-scoped; closes the currently-in-effect price for the (resource,
--: unit) tuple by stamping effective_to. Called by SetCurrentPrice in a
--: transaction with the new price insert so the swap is atomic.
UPDATE prices
SET effective_to = $4, updated_at = now()
WHERE tenant_id = $1
  AND resource_type = $2
  AND unit = $3
  AND effective_to IS NULL;

-- ===========================================================================
-- ledger_entries: append-only per-user ledger. INSERT + SELECT only.
-- ===========================================================================

-- name: CreateLedgerEntry :one
--: tenant-scoped; one row per credit (topup, refund) or debit (charge).
INSERT INTO ledger_entries (
    tenant_id, user_id, type, amount_cents, currency,
    source, reference, idempotency_key, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetLedgerEntryByID :one
--: tenant-scoped.
SELECT * FROM ledger_entries
WHERE tenant_id = $1 AND id = $2;

-- name: GetLedgerEntryByIdempotencyKey :one
--: tenant-scoped; used by the metering de-dup check before insert.
SELECT * FROM ledger_entries
WHERE tenant_id = $1 AND idempotency_key = $2;

-- name: ListLedgerEntriesForUser :many
--: tenant-scoped; paginated list of a single user's entries, newest first.
SELECT * FROM ledger_entries
WHERE tenant_id = $1 AND user_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountLedgerEntriesForUser :one
--: tenant-scoped.
SELECT count(*) FROM ledger_entries
WHERE tenant_id = $1 AND user_id = $2;

-- name: SumLedgerEntriesForUser :one
--: tenant-scoped; the authoritative balance computation. Returns the sum
--: of credits - debits in integer centimals. Cast to BIGINT explicitly so
--: sqlc emits int64 (SUM(BIGINT) would otherwise pick the default int32).
SELECT
    (COALESCE(SUM(CASE WHEN type = 'credit' THEN amount_cents ELSE 0 END), 0)
   - COALESCE(SUM(CASE WHEN type = 'debit'  THEN amount_cents ELSE 0 END), 0))::BIGINT
    AS balance_cents
FROM ledger_entries
WHERE tenant_id = $1 AND user_id = $2;

-- name: SumLedgerEntriesForUserUpTo :one
--: tenant-scoped; the balance as-of a timestamp. Used by the receipt
--: generator to compute the period's total charges.
SELECT
    (COALESCE(SUM(CASE WHEN type = 'credit' THEN amount_cents ELSE 0 END), 0)
   - COALESCE(SUM(CASE WHEN type = 'debit'  THEN amount_cents ELSE 0 END), 0))::BIGINT
    AS balance_cents
FROM ledger_entries
WHERE tenant_id = $1 AND user_id = $2 AND created_at <= sqlc.arg('as_of');

-- name: SumChargesInPeriod :one
--: tenant-scoped; total debits for a user in a [from, to] period. The
--: receipt generator uses this for the period's total charged. Cast to
--: BIGINT explicitly so sqlc emits int64.
SELECT COALESCE(SUM(amount_cents), 0)::BIGINT AS total_cents
FROM ledger_entries
WHERE tenant_id = @tenant_id
  AND user_id = @user_id
  AND type = 'debit'
  AND created_at >= sqlc.arg('from_ts')
  AND created_at <= sqlc.arg('to_ts');

-- ===========================================================================
-- user_balances: per-user balance cache. Mutable; the ledger post-processor
-- updates this after every insert.
-- ===========================================================================

-- name: GetUserBalance :one
--: tenant-scoped; returns the cached balance row for (tenant, user), or
--: no rows if no ledger entry has ever been written for the user.
SELECT * FROM user_balances
WHERE tenant_id = $1 AND user_id = $2;

-- name: UpsertUserBalance :exec
--: tenant-scoped; atomic cache refresh. amount_cents is the freshly-computed
--: balance; last_entry_at is the timestamp of the ledger entry that produced it.
INSERT INTO user_balances (tenant_id, user_id, balance_cents, currency, last_entry_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (tenant_id, user_id) DO UPDATE
SET balance_cents = EXCLUDED.balance_cents,
    currency      = EXCLUDED.currency,
    last_entry_at = EXCLUDED.last_entry_at,
    updated_at    = now();

-- name: ListZeroBalances :many
--: tenant-scoped admin helper for the enforcement job. Returns every user
--: in the tenant whose balance is <= 0 and whose last_entry_at is older
--: than the supplied cutoff (so the grace period is enforced).
SELECT * FROM user_balances
WHERE tenant_id = $1
  AND balance_cents <= 0
  AND last_entry_at <= $2
ORDER BY last_entry_at ASC;

-- ===========================================================================
-- usage_events: raw metering stream. INSERT + SELECT only.
-- ===========================================================================

-- name: CreateUsageEvent :one
--: tenant-scoped; one row per (tenant, user, resource, minute).
INSERT INTO usage_events (
    tenant_id, user_id, resource_type, qty, unit,
    started_at, ended_at, idempotency_key
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetUsageEventByIdempotencyKey :one
--: tenant-scoped; used by the metering de-dup check before insert.
SELECT * FROM usage_events
WHERE tenant_id = $1 AND idempotency_key = $2;

-- name: ListUsageEventsForUser :many
--: tenant-scoped; paginated, filterable by resource + date range.
SELECT * FROM usage_events
WHERE tenant_id = $1
  AND user_id = $2
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR started_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR started_at <= sqlc.narg('to_ts')::timestamptz)
ORDER BY started_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountUsageEventsForUser :one
--: tenant-scoped; pagination counterpart to ListUsageEventsForUser.
SELECT count(*) FROM usage_events
WHERE tenant_id = $1
  AND user_id = $2
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR started_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR started_at <= sqlc.narg('to_ts')::timestamptz);

-- name: SumUsageEventsForUserInPeriod :many
--: tenant-scoped; the rollup. Returns one row per (resource_type, unit) with
--: the total qty consumed in the [from, to] window. The rollup job then
--: joins this with prices to compute the charge.
SELECT resource_type, unit, SUM(qty) AS total_qty
FROM usage_events
WHERE tenant_id = @tenant_id
  AND user_id = @user_id
  AND started_at >= sqlc.arg('from_ts')
  AND started_at <= sqlc.arg('to_ts')
GROUP BY resource_type, unit;

-- ===========================================================================
-- receipts: per-user-per-period billing summary. Mutable (status + pdf flip).
-- ===========================================================================

-- name: CreateReceipt :one
--: tenant-scoped.
INSERT INTO receipts (
    tenant_id, user_id, period_start, period_end,
    total_cents, currency, pdf_bytes, status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetReceiptByID :one
--: tenant-scoped.
SELECT * FROM receipts
WHERE tenant_id = $1 AND id = $2;

-- name: ListReceiptsForUser :many
--: tenant-scoped; newest first.
SELECT * FROM receipts
WHERE tenant_id = $1 AND user_id = $2
ORDER BY period_start DESC
LIMIT $3 OFFSET $4;

-- name: CountReceiptsForUser :one
--: tenant-scoped.
SELECT count(*) FROM receipts
WHERE tenant_id = $1 AND user_id = $2;

-- name: GetReceiptForPeriod :one
--: tenant-scoped; used by the generator to detect an existing receipt.
SELECT * FROM receipts
WHERE tenant_id = $1
  AND user_id = $2
  AND period_start = $3
  AND period_end = $4;

-- name: UpdateReceiptPDF :exec
--: tenant-scoped; attaches the generated PDF + flips status to ready.
UPDATE receipts
SET pdf_bytes = $3, status = 'ready', updated_at = now()
WHERE tenant_id = $1 AND id = $2;
