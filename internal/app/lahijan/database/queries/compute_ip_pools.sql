-- IP pools (WS-30, ADR-0037): operator-owned pool metadata + per-pool
-- CIDR ranges. Global tables (no tenant_id) — the pool is the operator's
-- resource; tenants allocate from it via the floating_ips table
-- (compute_floating_ips.sql). Every query here is admin-only at the
-- HTTP boundary (RequirePerm(compute.ip_pool.manage)).

-- -------------------------------------------------------------------------
-- ip_pools
-- -------------------------------------------------------------------------

-- name: CreateIPPool :one
INSERT INTO ip_pools (name, description, ptr_zone_id, is_active)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetIPPoolByID :one
SELECT * FROM ip_pools WHERE id = $1 AND deleted_at IS NULL;

-- name: GetIPPoolByName :one
SELECT * FROM ip_pools WHERE name = $1 AND deleted_at IS NULL;

-- name: ListIPPools :many
SELECT * FROM ip_pools
WHERE deleted_at IS NULL
ORDER BY created_at ASC
LIMIT $1 OFFSET $2;

-- name: CountIPPools :one
SELECT count(*) FROM ip_pools WHERE deleted_at IS NULL;

-- name: UpdateIPPool :exec
-- Replaces the mutable fields. The name is immutable (other tables may
-- reference the pool by id, but operators identify pools by name and
-- renaming would break operator automation that scrapes by name).
UPDATE ip_pools
SET description = $2,
    ptr_zone_id = $3,
    is_active   = $4,
    updated_at  = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: SetIPPoolActive :exec
-- Convenience update for the activate/deactivate toggle.
UPDATE ip_pools
SET is_active = $2, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: SoftDeleteIPPool :exec
-- Marks the pool deleted_at = now(). The unique name index is partial
-- on deleted_at IS NULL so the name can be re-used after a soft-delete.
-- ON DELETE RESTRICT on floating_ips.pool_id prevents hard deletion via
-- SQL when allocations exist; the service layer must release every
-- allocation before soft-deleting.
UPDATE ip_pools
SET deleted_at = now(), is_active = FALSE, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- -------------------------------------------------------------------------
-- ip_pool_ranges
-- -------------------------------------------------------------------------

-- name: CreateIPPoolRange :one
INSERT INTO ip_pool_ranges (pool_id, cidr, family, excluded_addresses)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetIPPoolRangeByID :one
SELECT * FROM ip_pool_ranges WHERE id = $1;

-- name: GetIPPoolRangeByPoolCIDR :one
-- Natural-key lookup so the service can detect "this CIDR is already in
-- the pool" without a separate scan.
SELECT * FROM ip_pool_ranges WHERE pool_id = $1 AND cidr = $2;

-- name: ListIPPoolRanges :many
SELECT * FROM ip_pool_ranges
WHERE pool_id = $1
ORDER BY family ASC, cidr ASC
LIMIT $2 OFFSET $3;

-- name: ListAllIPPoolRangesForPool :many
-- Unpaginated variant used by the allocation logic so the service can
-- walk every range in a single query when looking for the next free IP.
SELECT * FROM ip_pool_ranges
WHERE pool_id = $1
ORDER BY family ASC, cidr ASC;

-- name: CountIPPoolRanges :one
SELECT count(*) FROM ip_pool_ranges WHERE pool_id = $1;

-- name: DeleteIPPoolRange :exec
-- Hard-delete; the range row carries no historical audit value once
-- removed. The unique constraint is released so the same CIDR can be
-- re-added later. Existing floating_ips allocations inside the range
-- remain valid (they reference pool_id, not range_id).
DELETE FROM ip_pool_ranges WHERE pool_id = $1 AND id = $2;
