-- DNS domains: tenant-scoped domain registration lifecycle (WS-28).
--
-- Every query that reads user data is tenant-scoped via WithTenant
-- (database/tenant.go). The two exceptions are:
--
--   * GetDNSDomainByOrderGlobal — admin-only cross-tenant lookup used by
--     the registrar service to deduplicate orders across tenants.
--   * SetDNSDomainStatus — internal lifecycle transition; the service
--     layer asserts tenant scope before calling.

-- name: CreateDNSDomain :one
--: tenant-scoped
INSERT INTO dns_domains (
    tenant_id, name, status, registrar_order_id, contact_profile_id,
    zone_id, price_cents, currency, period_years, ledger_entry_id,
    is_dnssec_enabled, is_auto_renew, registered_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetDNSDomainByID :one
--: tenant-scoped
SELECT * FROM dns_domains
WHERE tenant_id = $1 AND id = $2;

-- name: GetDNSDomainByName :one
--: tenant-scoped
-- Returns the row only when the canonical domain name is owned by the
-- tenant in ctx. Used by the registrar service on every privileged call
-- to enforce tenant isolation at the repository seam.
SELECT * FROM dns_domains WHERE tenant_id = $1 AND name = $2;

-- name: GetDNSDomainByOrderGlobal :one
-- Admin-only path: no tenant scoping. Used by the registrar service's
-- cross-tenant "is this order already tracked?" lookup at registration
-- time so two tenants cannot double-claim the same order id.
SELECT * FROM dns_domains WHERE registrar_order_id = $1;

-- name: ListDNSDomains :many
--: tenant-scoped
SELECT * FROM dns_domains
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountDNSDomains :one
--: tenant-scoped
SELECT count(*) FROM dns_domains WHERE tenant_id = $1;

-- name: SetDNSDomainStatus :exec
--: tenant-scoped
-- Flips the lifecycle status. Called by the registrar service after a
-- register / renew / transfer / expire transition.
UPDATE dns_domains
SET status = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainZoneLink :exec
--: tenant-scoped
-- Links the row to the auto-provisioned dns_zones row.
UPDATE dns_domains
SET zone_id = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainLedgerLink :exec
--: tenant-scoped
-- Records the ledger entry id that paid for the registration / renewal.
UPDATE dns_domains
SET ledger_entry_id = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainExpiry :exec
--: tenant-scoped
-- Updates the registered_at + expires_at timestamps after a successful
-- register / renew. registered_at is only set when the caller supplies
-- a non-null value (renew keeps the original registration date).
UPDATE dns_domains
SET registered_at = COALESCE($3, registered_at),
    expires_at    = $4,
    updated_at    = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainAutoRenew :exec
--: tenant-scoped
UPDATE dns_domains
SET is_auto_renew = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainDNSSECCached :exec
--: tenant-scoped
-- Flips the cached is_dnssec_enabled flag. Called by the registrar
-- service after a successful publish-DS + sign-zone composition.
UPDATE dns_domains
SET is_dnssec_enabled = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSDomainOrderID :exec
--: tenant-scoped
-- Records the registrar's order id after a successful register / transfer.
UPDATE dns_domains
SET registrar_order_id = $3, status = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteDNSDomain :exec
--: tenant-scoped
DELETE FROM dns_domains WHERE tenant_id = $1 AND id = $2;

-- name: ListDNSDomainsExpiringBefore :many
--: tenant-scoped
-- Returns every domain in the tenant whose expires_at is before the
-- supplied timestamp. Used by the future renewal job to drive auto-renew.
SELECT * FROM dns_domains
WHERE tenant_id = $1 AND expires_at IS NOT NULL AND expires_at <= $2
ORDER BY expires_at ASC;
