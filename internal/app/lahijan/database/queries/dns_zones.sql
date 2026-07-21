-- DNS zones: tenant-scoped mapping (WS-12). The PowerDNS driver operates on
-- the canonical zone id; the DNS service (WS-15) consults this table to
-- translate a tenant context into the canonical id. Every query is
-- tenant-scoped via WithTenant (database/tenant.go) EXCEPT the admin-only
-- "canonical id -> row" lookup which is global.

-- name: CreateDNSZone :one
--: tenant-scoped
INSERT INTO dns_zones (tenant_id, canonical_id, name, kind, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetDNSZoneByID :one
--: tenant-scoped
SELECT * FROM dns_zones
WHERE tenant_id = $1 AND id = $2;

-- name: GetDNSZoneByCanonical :one
-- Admin-only path: no tenant scoping. Used by the DNS service's
-- cross-tenant "is this canonical id owned by anyone?" check.
SELECT * FROM dns_zones WHERE canonical_id = $1;

-- name: GetDNSZoneByIDGlobal :one
-- Admin-only path: no tenant scoping. Used by the WS-30 PTR publisher
-- (program/compute_ip_ptrs.go) to look up the operator-owned reverse
-- zone by id without knowing which tenant owns it.
SELECT * FROM dns_zones WHERE id = $1;

-- name: GetDNSZoneByCanonicalForTenant :one
--: tenant-scoped
-- Tenant-scoped variant: returns the row only if the canonical id is owned
-- by the given tenant. Used by the DNS service on every privileged call to
-- enforce tenant isolation at the repository seam.
SELECT * FROM dns_zones WHERE tenant_id = $1 AND canonical_id = $2;

-- name: ListDNSZones :many
--: tenant-scoped
SELECT * FROM dns_zones
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountDNSZones :one
--: tenant-scoped
SELECT count(*) FROM dns_zones WHERE tenant_id = $1;

-- name: SetDNSZoneDNSSECCached :exec
--: tenant-scoped
-- Flips the cached is_dnssec_enabled flag. Called by the DNS service after
-- a successful EnableDNSSEC / DisableDNSSEC against PDNS.
UPDATE dns_zones
SET is_dnssec_enabled = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetDNSZoneAXFRCached :exec
--: tenant-scoped
UPDATE dns_zones
SET is_axfr_enabled = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: UpdateDNSZoneDescription :exec
--: tenant-scoped
UPDATE dns_zones
SET description = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: UpdateDNSZoneKind :exec
--: tenant-scoped
UPDATE dns_zones
SET kind = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteDNSZone :exec
--: tenant-scoped
DELETE FROM dns_zones WHERE tenant_id = $1 AND id = $2;
