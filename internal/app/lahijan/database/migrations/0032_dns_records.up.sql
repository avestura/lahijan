-- 0032_dns_records: per-tenant DNS records (WS-15).
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns. dns_records is the canonical Lahijan-side record of a
-- single RR within a zone: the row mirrors the PDNS RRset entry but is
-- authored in our DB so the API can list, audit, and template-apply without
-- a round-trip per record. The canonical (name, type, content) triple is
-- scoped to (tenant_id, zone_id) so the same name in two tenants stays
-- isolated.
--
-- The row NEVER holds the live PDNS state directly — the DNS service upserts
-- the matching RRset on every change. The row is the source of truth on the
-- Lahijan side; PDNS is derived.
--
-- zone_id is a FK into dns_zones (which already cascades on tenant delete).
-- A soft-delete pattern is NOT used here: deleting an RR deletes the row,
-- because every historical query goes through the audit log instead. This
-- keeps the table small and the unique constraint honest.
--
-- content_text / content_blob: PDNS stores RRs as opaque strings; we keep
-- the canonical zone-file wire form (e.g. "192.0.2.1" for an A record,
-- "10 mail.example.com." for an MX). The record-type-specific validators
-- (service layer) gate writes before they reach this table.

CREATE TABLE dns_records (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- FK into dns_zones; ON DELETE CASCADE so a zone delete drops every
    -- owned record (the service also issues a PDNS zone delete first).
    zone_id         UUID        NOT NULL REFERENCES dns_zones (id) ON DELETE CASCADE,
    -- Canonical record name with trailing dot (e.g. "www.example.com.").
    -- Always lowercase; the validator enforces both at write time.
    name            TEXT        NOT NULL,
    -- DNS record type ("A", "AAAA", "CNAME", "MX", "TXT", "NS", "SOA",
    -- "SRV", "CAA", "PTR"). A CHECK would lock us to the current enum;
    -- we keep it loose so an upgrade does not require a migration. The
    -- service-layer allowlist is authoritative.
    type            TEXT        NOT NULL,
    -- Zone-file wire form (e.g. "192.0.2.1" for A, "10 mail.example.com."
    -- for MX). Validated by record-type-specific validators at write time.
    content         TEXT        NOT NULL,
    -- TTL in seconds; defaults to 3600. The service clamps to [300, 86400]
    -- per WS-15 "Open questions" item 1.
    ttl             INT         NOT NULL DEFAULT 3600,
    -- Priority for MX / SRV; 0 for types that have no priority. PDNS reads
    -- it from the leading integer in the content for those types but we
    -- store it as a separate column for fast filtering.
    prio            INT         NOT NULL DEFAULT 0,
    -- Disabled records are served by PDNS as "commented out" — they remain
    -- in the zone file but are not authoritative. The UI surfaces this as
    -- a per-record toggle.
    disabled        BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot lookups: per-tenant list, per-zone list, (tenant, zone, name, type)
-- uniqueness so two RRs cannot collide on the same identity.
CREATE INDEX        idx_dns_records_tenant_id     ON dns_records (tenant_id);
CREATE INDEX        idx_dns_records_zone_id       ON dns_records (zone_id);
CREATE INDEX        idx_dns_records_tenant_zone   ON dns_records (tenant_id, zone_id);
CREATE UNIQUE INDEX uq_dns_records_zone_name_type_content
    ON dns_records (zone_id, name, type, content);

COMMENT ON TABLE  dns_records          IS 'Per-tenant DNS records. Mirrors PDNS RRsets; the DNS service (WS-15) upserts on every change.';
COMMENT ON COLUMN dns_records.zone_id  IS 'FK into dns_zones; cascades on zone delete.';
COMMENT ON COLUMN dns_records.name     IS 'Canonical record name with trailing dot; always lowercase.';
COMMENT ON COLUMN dns_records.type     IS 'DNS record type (A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, CAA, PTR). Validated at the service layer.';
COMMENT ON COLUMN dns_records.content  IS 'Zone-file wire form. Validated per-type by the DNS service.';
COMMENT ON COLUMN dns_records.ttl      IS 'TTL in seconds; clamped to [300, 86400] by the service.';
COMMENT ON COLUMN dns_records.prio     IS 'Priority for MX / SRV; 0 for types that have no priority.';
COMMENT ON COLUMN dns_records.disabled IS 'When true, PDNS serves the record as commented-out.';
