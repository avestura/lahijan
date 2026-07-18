-- 0026_dns_zones: tenant -> zone mapping for the PowerDNS provider (WS-12).
--
-- Per ADR-0002 every tenant-scoped table has tenant_id + the standard
-- audit columns. dns_zones is the canonical mapping the DNS service (WS-15)
-- consults to translate a tenant context into the canonical zone id before
-- calling the PowerDNS driver. The PDNS zone itself lives in the pdns
-- logical database (ADR-0007); this table records only Lahijan's view of
-- ownership.
--
-- The canonical_id column is the PDNS-assigned zone id (typically the
-- canonical zone name lowercased). The name column is the human-readable
-- canonical zone name ("example.com."). Both are unique per row.

CREATE TABLE dns_zones (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- PDNS-assigned canonical id (e.g. "example.com."). Unique across the
    -- platform because two tenants cannot own the same zone.
    canonical_id    TEXT        NOT NULL,
    -- human-readable canonical name with trailing dot.
    name            TEXT        NOT NULL,
    -- "Native", "Master", or "Slave". Lahijan always uses "Native" today.
    kind            TEXT        NOT NULL DEFAULT 'Native',
    -- is_dnssec_enabled caches the PDNS-side DNSSEC state so the admin UI
    -- does not need to round-trip to PDNS to render the toggle. The DNS
    -- service flips this when EnableDNSSEC / DisableDNSSEC succeeds.
    is_dnssec_enabled BOOLEAN    NOT NULL DEFAULT FALSE,
    -- is_axfr_enabled caches whether ALLOW-AXFR-FROM is set. Off by
    -- default per WS-12 "Open questions" item 2.
    is_axfr_enabled BOOLEAN     NOT NULL DEFAULT FALSE,
    -- optional description the user supplied at zone-create time.
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- canonical_id is globally unique (no two tenants can own the same zone).
CREATE UNIQUE INDEX uq_dns_zones_canonical_id ON dns_zones (canonical_id);
-- (tenant_id, name) is unique within a tenant.
CREATE UNIQUE INDEX uq_dns_zones_tenant_name  ON dns_zones (tenant_id, name);
-- Hot lookups for the per-tenant list + the "is this zone owned by tenant X"
-- check the DNS service performs on every privileged call.
CREATE INDEX        idx_dns_zones_tenant_id   ON dns_zones (tenant_id);

COMMENT ON TABLE  dns_zones              IS 'Tenant -> PDNS zone ownership mapping. Enforced by the DNS service (WS-15).';
COMMENT ON COLUMN dns_zones.canonical_id IS 'PDNS-assigned zone id; globally unique because two tenants cannot own the same zone.';
COMMENT ON COLUMN dns_zones.kind         IS 'Zone kind: Native | Master | Slave. Lahijan uses Native by default.';
COMMENT ON COLUMN dns_zones.is_dnssec_enabled IS 'Cached DNSSEC state; flipped by the DNS service on EnableDNSSEC / DisableDNSSEC.';
COMMENT ON COLUMN dns_zones.is_axfr_enabled   IS 'Cached AXFR state; off by default per WS-12.';

-- Add a CHECK on kind so a typo cannot strand a row in an unknown state.
ALTER TABLE dns_zones ADD CONSTRAINT dns_zones_kind_valid
    CHECK (kind IN ('Native', 'Master', 'Slave'));
