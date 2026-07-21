-- dns_records (WS-15): tenant-scoped records for the DNS module. Every
-- query here filters by tenant_id (set by WithTenant at the repo seam).
-- The zone_id is always supplied by the caller (the DNS service resolves
-- it from the dns_zones table first); tenant + zone scoping together
-- enforce isolation at the repository boundary.
--
-- Rows are NOT soft-deleted: deleting an RR removes the row because every
-- historical query goes through audit_log instead. This keeps the unique
-- constraint honest and the table small.

-- name: CreateDNSRecord :one
--: tenant-scoped
INSERT INTO dns_records (
    tenant_id,
    zone_id,
    name,
    type,
    content,
    ttl,
    prio,
    disabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetDNSRecordByID :one
--: tenant-scoped
SELECT * FROM dns_records
WHERE tenant_id = $1 AND id = $2;

-- name: GetDNSRecordByIdentity :one
--: tenant-scoped; looks up by (zone_id, name, type, content) — the unique
--: identity of an RR. Used by the DNS service to short-circuit "this RR
--: already exists" before issuing a PDNS REPLACE.
SELECT * FROM dns_records
WHERE tenant_id = $1 AND zone_id = $2 AND name = $3 AND type = $4 AND content = $5;

-- name: GetDNSRecordByNameGlobal :one
-- Admin-only path: no tenant scoping. Used by the WS-30 PTR publisher
-- (program/compute_ip_ptrs.go) to find the PTR record for an IP in the
-- operator-owned reverse zone without knowing which tenant owns the
-- zone. The (zone_id, name, type) tuple is unique by construction
-- (a zone has one PTR per name) so the lookup is deterministic.
SELECT * FROM dns_records
WHERE zone_id = $1 AND name = $2 AND type = $3;

-- name: ListDNSRecordsInZone :many
--: tenant-scoped; returns every RR in the zone, ordered by (name, type)
--: so the UI renders a stable list.
SELECT * FROM dns_records
WHERE tenant_id = $1 AND zone_id = $2
ORDER BY name ASC, type ASC
LIMIT $3 OFFSET $4;

-- name: CountDNSRecordsInZone :one
--: tenant-scoped
SELECT count(*) FROM dns_records
WHERE tenant_id = $1 AND zone_id = $2;

-- name: CountDNSRecordsForTenant :one
--: tenant-scoped; the quota checker uses this to enforce the per-tenant
--: record cap.
SELECT count(*) FROM dns_records
WHERE tenant_id = $1;

-- name: UpdateDNSRecord :exec
--: tenant-scoped; replaces content / ttl / prio / disabled. The name and
--: type are immutable — callers wanting a "rename" issue a delete + create
--: so the audit trail stays honest.
UPDATE dns_records
SET content = $3, ttl = $4, prio = $5, disabled = $6, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteDNSRecord :exec
--: tenant-scoped
DELETE FROM dns_records
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteAllDNSRecordsInZone :exec
--: tenant-scoped; used by the zone-delete path so the FK cascade is
--: explicit even before the zone row goes away.
DELETE FROM dns_records
WHERE tenant_id = $1 AND zone_id = $2;
