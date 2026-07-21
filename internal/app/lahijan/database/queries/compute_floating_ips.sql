-- Floating IPs (WS-30, ADR-0037): per-tenant public-IP allocations.
-- Every query is tenant-scoped via WithTenant (database/tenant.go) so a
-- cross-tenant floating_ip_id surfaces as ErrNoRows, never as the row
-- itself. The address column is INET; allocation logic computes the next
-- free address in Go (using netip arithmetic over the pool's ranges) and
-- inserts the resolved host address via CreateFloatingIP.

-- name: CreateFloatingIP :one
--: tenant-scoped
-- Inserts a new allocation. The service layer computes the address
-- (next-free IP from the pool's ranges minus the existing allocations)
-- before this call; the unique index on (address) WHERE deleted_at IS
-- NULL guards against a race between two concurrent allocations
-- (sqlc/pgx surfaces a unique-violation the service maps to 409).
INSERT INTO floating_ips (
    tenant_id, pool_id, address, family,
    ptr_target, instance_id, network_name, forward_push_status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetFloatingIPByID :one
--: tenant-scoped
SELECT * FROM floating_ips
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetFloatingIPByAddress :one
--: tenant-scoped
-- Used by the service layer to detect "this address is already
-- allocated to the tenant" without a separate scan. Cross-tenant
-- collisions are caught by the global unique index on (address).
SELECT * FROM floating_ips
WHERE tenant_id = $1 AND address = $2 AND deleted_at IS NULL;

-- name: ListFloatingIPs :many
--: tenant-scoped
SELECT * FROM floating_ips
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountFloatingIPs :one
--: tenant-scoped
SELECT count(*) FROM floating_ips
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: ListFloatingIPsByPool :many
--: tenant-scoped
-- Used by the service layer's free/allocated counter for the operator
-- UI. Paginated by (tenant, pool).
SELECT * FROM floating_ips
WHERE tenant_id = $1 AND pool_id = $2 AND deleted_at IS NULL
ORDER BY address ASC
LIMIT $3 OFFSET $4;

-- name: CountFloatingIPsByPool :one
--: tenant-scoped
SELECT count(*) FROM floating_ips
WHERE tenant_id = $1 AND pool_id = $2 AND deleted_at IS NULL;

-- name: GetFloatingIPByInstance :one
--: tenant-scoped
-- Returns the floating IP currently attached to the given instance, if
-- any. Used by the instance-detail "attached IP" card.
SELECT * FROM floating_ips
WHERE tenant_id = $1 AND instance_id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListAllAddressesForTenant :many
--: tenant-scoped
-- Returns the addresses (INET column projected to TEXT) of every
-- non-deleted allocation in the tenant. Used by the allocation logic
-- to compute the set of already-allocated addresses without dragging
-- the whole row.
SELECT address::text AS address FROM floating_ips
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: ListAllAddressesInPool :many
-- Global (not tenant-scoped): the operator's free/allocated counter
-- needs the count across every tenant, not just the caller's tenant.
-- Used only by the admin path (RequirePerm(compute.ip_pool.manage)).
SELECT address::text AS address FROM floating_ips
WHERE pool_id = $1 AND deleted_at IS NULL;

-- name: SetFloatingIPInstance :exec
--: tenant-scoped
-- Attaches (instance_id != NULL) or detaches (instance_id == NULL) the
-- floating IP. The service layer pushes the Incus forward on attach
-- (best-effort) and removes it on detach before flipping this column.
UPDATE floating_ips
SET instance_id = $3,
    forward_push_status = $4,
    network_name = $5,
    updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetFloatingIPPTRTarget :exec
--: tenant-scoped
-- Replaces the ptr_target. The service layer re-publishes the PTR
-- record into the pool's ptr_zone (or the in-addr.arpa zone the tenant
-- owns) on every change.
UPDATE floating_ips
SET ptr_target = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetFloatingIPForwardPushStatus :exec
--: tenant-scoped
-- Convenience update for the best-effort forward push path so the
-- audit trail + the operator UI can render the push outcome without
-- rewriting the rest of the row.
UPDATE floating_ips
SET forward_push_status = $3, network_name = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteFloatingIP :exec
--: tenant-scoped
-- Marks the allocation deleted_at = now(). The unique index on (address)
-- WHERE deleted_at IS NULL releases so the address can be re-allocated
-- after release. The service layer removes the Incus forward +
-- publishes the PTR-record delete into the pool's ptr_zone before this.
UPDATE floating_ips
SET deleted_at = now(),
    instance_id = NULL,
    network_name = NULL,
    forward_push_status = 'pending',
    updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
