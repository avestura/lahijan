-- 0048_compute_ip_pools_floating_ips: operator-owned IP pools +
-- per-tenant floating IPs (WS-30, ADR-0037).
--
-- This migration adds the public-IP assignment + floating-IP surface:
--
--   * ip_pools        : operator-owned pool metadata (GLOBAL — no tenant_id;
--                       appears in the glossary's global-tables row alongside
--                       tenants / users / roles).
--   * ip_pool_ranges  : per-pool CIDR ranges (v4 + v6). Stored as TEXT in
--                       canonical netip.Prefix.String() form so the Go side
--                       parses with netip.ParsePrefix without any inet/cidr
--                       type juggling at the sqlc seam.
--   * floating_ips    : per-tenant allocation. Tenant-scoped; one row per
--                       allocated public IP. Carries the ptr_target +
--                       optional instance_id (the attach target) + the
--                       best-effort Incus forward push status.
--
-- Per ADR-0037 the Lahijan side is the source of truth for the allocation
-- (the floating_ips row). The Incus side (network forward or proxy device)
-- is derived and pushed opportunistically — a failed push does NOT roll
-- back the allocation because the operator may be using external plumbing
-- (BGP via FRR, static route, ...). The push outcome is recorded in
-- floating_ips.forward_push_status so the operator UI can render it.

-- -------------------------------------------------------------------------
-- ip_pools: operator-owned pool metadata. Global (no tenant_id).
-- -------------------------------------------------------------------------

CREATE TABLE ip_pools (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT        NOT NULL,
    description  TEXT,
    -- ptr_zone_id: optional zone in dns_zones to publish PTR records into.
    -- When set, every floating IP allocated from this pool publishes a PTR
    -- record into this zone on attach (and removes it on release). NULL
    -- means PTR auto-publish is disabled for the pool — the tenant must
    -- own the in-addr.arpa zone separately, or no reverse DNS is desired.
    -- ON DELETE SET NULL: deleting the zone disables auto-publish but does
    -- not strand the pool.
    ptr_zone_id  UUID        REFERENCES dns_zones (id) ON DELETE SET NULL,
    is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

-- name is unique among non-deleted pools (so two operators can re-use a
-- decommissioned name after a soft-delete).
CREATE UNIQUE INDEX uq_ip_pools_name    ON ip_pools (name) WHERE deleted_at IS NULL;
CREATE INDEX        idx_ip_pools_active ON ip_pools (is_active) WHERE deleted_at IS NULL;

COMMENT ON TABLE  ip_pools             IS 'Operator-owned IP pool (WS-30, ADR-0037). Global table; the pool is the source of allocatable public addresses.';
COMMENT ON COLUMN ip_pools.name        IS 'Operator-chosen pool identifier; unique among non-deleted pools.';
COMMENT ON COLUMN ip_pools.ptr_zone_id IS 'Optional dns_zones.id to publish PTR records into on every attach. NULL disables auto-publish for the pool.';
COMMENT ON COLUMN ip_pools.is_active   IS 'When FALSE, tenants cannot allocate new floating IPs from this pool; existing allocations remain.';

-- -------------------------------------------------------------------------
-- ip_pool_ranges: per-pool CIDR ranges.
-- -------------------------------------------------------------------------

CREATE TABLE ip_pool_ranges (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id     UUID        NOT NULL REFERENCES ip_pools (id) ON DELETE CASCADE,

    -- cidr is the network prefix in canonical netip.Prefix form (e.g.
    -- "203.0.113.0/24", "2001:db8::/32"). Stored as TEXT so the Go side
    -- parses with netip.ParsePrefix without inet/cidr juggling.
    cidr        TEXT        NOT NULL,

    -- family: 4 or 6 (derived from cidr at insert time; cached here so
    -- the per-family allocation queries can index without parsing the
    -- prefix on every read).
    family      INT         NOT NULL,

    -- excluded_addresses: addresses inside the range that are NOT
    -- allocatable (network address, broadcast, gateway, reserved, ...).
    -- Stored as a JSONB array of IP-string literals
    -- (e.g. ["203.0.113.0", "203.0.113.1", "203.0.113.255"]) so the Go
    -- side parses with netip.ParseAddr. Small list by design — a /24 has
    -- at most a handful of exclusions.
    excluded_addresses JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE ip_pool_ranges ADD CONSTRAINT ip_pool_ranges_family_valid
    CHECK (family IN (4, 6));

-- (pool_id, cidr) is unique so the same range cannot be added twice to
-- the same pool. Two pools can each have the same CIDR (the operator is
-- responsible for not double-registering ranges across pools).
CREATE UNIQUE INDEX uq_ip_pool_ranges_pool_cidr ON ip_pool_ranges (pool_id, cidr);
CREATE INDEX        idx_ip_pool_ranges_pool     ON ip_pool_ranges (pool_id);

COMMENT ON TABLE  ip_pool_ranges                  IS 'Per-pool CIDR ranges (WS-30, ADR-0037). A pool can hold multiple ranges (v4 + v6).';
COMMENT ON COLUMN ip_pool_ranges.cidr             IS 'Network prefix in netip.Prefix canonical form (e.g. "203.0.113.0/24").';
COMMENT ON COLUMN ip_pool_ranges.family           IS 'Address family (4 or 6); cached from cidr for indexing.';
COMMENT ON COLUMN ip_pool_ranges.excluded_addresses IS 'JSONB array of IP strings inside the range that are NOT allocatable (network, broadcast, gateway, ...).';

-- -------------------------------------------------------------------------
-- floating_ips: per-tenant allocation. Tenant-scoped.
-- -------------------------------------------------------------------------

CREATE TABLE floating_ips (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    pool_id     UUID        NOT NULL REFERENCES ip_pools (id) ON DELETE RESTRICT,

    -- address is the public IP; stored as INET for arithmetic + global
    -- uniqueness. The host bits are always zero-or-prefix (the allocation
    -- always picks a host address, never a network or broadcast).
    address     INET        NOT NULL,

    -- family: 4 or 6 (cached from address for indexing).
    family      INT         NOT NULL,

    -- ptr_target is the FQDN the reverse-DNS auto-publish writes into the
    -- pool's ptr_zone (or the per-tenant in-addr.arpa zone if the pool
    -- has no ptr_zone_id). NULL means no PTR record is published.
    ptr_target  TEXT,

    -- instance_id: the instance the floating IP is currently attached to.
    -- NULL means allocated but not attached (the "floating" state).
    -- ON DELETE SET NULL: deleting the instance detaches the floating IP;
    -- the allocation stays with the tenant.
    instance_id UUID        REFERENCES compute_instances (id) ON DELETE SET NULL,

    -- network_name: the Incus network the forward was created on (when
    -- best-effort push is enabled). NULL means no forward has been
    -- pushed (single-node daemon without a managed bridge, or the push
    -- failed / is unsupported by the topology).
    network_name TEXT,

    -- forward_push_status: records the best-effort Incus forward push
    -- outcome so the audit trail and the operator UI can render whether
    -- Lahijan attempted the forward.
    --   pending     : newly allocated; push has not been attempted yet.
    --   pushed      : the forward was accepted by the Incus daemon.
    --   failed      : the Incus daemon rejected the push; the operator's
    --                 external automation may still be plumbing the route.
    --   unsupported : Lahijan decided not to push (no network configured
    --                 for the pool, or single-node daemon topology).
    forward_push_status TEXT NOT NULL DEFAULT 'pending',

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

ALTER TABLE floating_ips ADD CONSTRAINT floating_ips_family_valid
    CHECK (family IN (4, 6));
ALTER TABLE floating_ips ADD CONSTRAINT floating_ips_forward_status_valid
    CHECK (forward_push_status IN ('pending', 'pushed', 'failed', 'unsupported'));

-- Address is globally unique among non-deleted allocations (one
-- allocation per public IP at a time). The unique index is partial on
-- deleted_at IS NULL so a released IP can be re-allocated after a soft-
-- delete (the operator UI shows the historical row via the audit log).
CREATE UNIQUE INDEX uq_floating_ips_address ON floating_ips (address) WHERE deleted_at IS NULL;

-- Hot lookups:
--  * per-tenant list (the dashboard "floating IPs" panel)
--  * per-instance lookup (the instance-detail "attached IP" card)
--  * per-pool free/allocated counter (the operator UI)
CREATE INDEX idx_floating_ips_tenant   ON floating_ips (tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_floating_ips_instance ON floating_ips (instance_id)
    WHERE instance_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_floating_ips_pool     ON floating_ips (pool_id) WHERE deleted_at IS NULL;

COMMENT ON TABLE  floating_ips                       IS 'Per-tenant public-IP allocation (WS-30, ADR-0037). Source of truth for floating-IP state; the Incus forward is derived + best-effort.';
COMMENT ON COLUMN floating_ips.pool_id               IS 'The pool the address was allocated from. ON DELETE RESTRICT: dropping a pool with live allocations requires explicit release first.';
COMMENT ON COLUMN floating_ips.address               IS 'The allocated public IP (INET). Globally unique among non-deleted rows.';
COMMENT ON COLUMN floating_ips.family                IS 'Address family (4 or 6); cached from address for indexing.';
COMMENT ON COLUMN floating_ips.ptr_target            IS 'Optional FQDN to publish as the PTR target on attach. NULL disables PTR auto-publish for this allocation.';
COMMENT ON COLUMN floating_ips.instance_id           IS 'The attached instance; NULL when the IP is allocated but not attached (floating).';
COMMENT ON COLUMN floating_ips.network_name          IS 'The Incus network the forward was created on, if any.';
COMMENT ON COLUMN floating_ips.forward_push_status   IS 'Best-effort Incus forward push status: pending | pushed | failed | unsupported.';
