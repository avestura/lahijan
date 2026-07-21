-- 0046_dns_domains: tenant-scoped domain registrations (WS-28).
--
-- Tracks the lifecycle of every domain a tenant has registered, renewed,
-- or transferred through Lahijan's registrar resale surface. Each row
-- pairs a domain name ("example.com.") with:
--
--   * the registrar order id (so the operator can match a Lahijan row
--     to a row in their registrar's back-office),
--   * the current lifecycle status (available / registered / pending /
--     transferred / expired),
--   * the optional link to the dns_zones row that hosts the zone once
--     the registration completes (Lahijan auto-provisions the zone on
--     a successful register via the WS-15 CreateZone flow),
--   * the cached pricing snapshot (price_cents + currency + period_years)
--     so receipts can render the original price even if the catalog
--     changes later,
--   * the billing ledger entry id (so the audit trail can reconstruct
--     "registration X was charged via ledger row Y"),
--   * the cached DNSSEC state (DS published at the parent + zone signed)
--     so the UI can render the toggle without a registrar round-trip.
--
-- Per ADR-0002 every tenant-scoped table has tenant_id + the standard
-- audit columns. The registrar order id is unique per row because two
-- tenants cannot own the same domain name through the same registrar
-- account (the registrar would reject the second order).

CREATE TABLE dns_domains (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,

    -- The canonical domain name with a trailing dot ("example.com.").
    -- Lowercase enforced at the service layer + by the unique index below.
    name                TEXT        NOT NULL,

    -- Lifecycle status. The set mirrors the OpenSRS / ResellerClub order
    -- states we care about; the service layer translates registrar-
    -- specific states into these.
    --   available  : the domain is queryable but not owned by us
    --   registered : we own it; auto-renew may or may not be on
    --   pending    : a register / renew / transfer is in flight
    --   transferred: the domain was transferred in from another registrar
    --   expired    : the registration lapsed; we no longer control it
    status              TEXT        NOT NULL DEFAULT 'available',

    -- Registrar-specific order id (OpenSRS order_id, ResellerClub
    -- orderid, ...). Surfaces in the audit row so the operator can
    -- cross-reference the registrar's back-office.
    registrar_order_id  TEXT        NOT NULL DEFAULT '',

    -- The registrar-specific identifier for the contact profile used
    -- at registration time. Surfaces in the audit row.
    -- Empty when the default contact profile was used.
    contact_profile_id  TEXT        NOT NULL DEFAULT '',

    -- Link to the dns_zones row that hosts the zone once the
    -- registration completes. NULL until Lahijan auto-provisions the
    -- zone (which happens inside the registrar service's post-register
    -- step). FK is nullable so a row can exist before the zone does.
    zone_id             UUID        NULL REFERENCES dns_zones (id) ON DELETE SET NULL,

    -- Cached pricing snapshot. The price at registration time is frozen
    -- so receipts render the original even if the catalog changes.
    price_cents         BIGINT      NOT NULL DEFAULT 0,
    currency            TEXT        NOT NULL DEFAULT 'USD',
    period_years        INT         NOT NULL DEFAULT 1,

    -- The ledger entry id that paid for the registration. Cross-
    -- references ledger_entries.id so the audit trail can reconstruct
    -- "registration X was charged via ledger row Y". NULL when the row
    -- represents a search result (status=available).
    ledger_entry_id     UUID        NULL,

    -- Cached DNSSEC automation state. Updated by the registrar service
    -- when the user toggles DNSSEC for a registered domain; the toggle
    -- composes the dns.zone.dnssec.enable path (WS-15) with the
    -- registrar's "publish DS at parent" call.
    is_dnssec_enabled   BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Auto-renew toggle. When true, the renewal job (a future WS)
    -- charges the user's balance + renews the domain before expiry.
    -- Off by default; the user opts in via the UI.
    is_auto_renew       BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Registration + expiry timestamps. expires_at is sourced from the
    -- registrar's response (which is the authoritative value). NULL
    -- until the registration completes.
    registered_at       TIMESTAMPTZ NULL,
    expires_at          TIMESTAMPTZ NULL,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (tenant_id, name) is unique within a tenant so the same domain cannot
-- be tracked twice in the same tenant. The platform-wide uniqueness
-- invariant is enforced by the registrar itself (a second tenant that
-- tries to register an already-registered domain gets a 409 from the
-- registrar before Lahijan writes a row).
CREATE UNIQUE INDEX uq_dns_domains_tenant_name ON dns_domains (tenant_id, name);

-- registrar_order_id is unique per row (cross-tenant) so the audit trail
-- can match a Lahijan row to a registrar back-office record. Empty
-- string is allowed for the search-result rows that have no order yet
-- (the partial index skips those).
CREATE UNIQUE INDEX uq_dns_domains_order_id
    ON dns_domains (registrar_order_id)
    WHERE registrar_order_id <> '';

-- Hot lookups for the per-tenant list + the "is this domain owned by
-- tenant X" check the registrar service performs on every privileged call.
CREATE INDEX idx_dns_domains_tenant_id   ON dns_domains (tenant_id);
CREATE INDEX idx_dns_domains_expires_at  ON dns_domains (tenant_id, expires_at)
    WHERE expires_at IS NOT NULL;

COMMENT ON TABLE  dns_domains                IS 'Per-tenant domain registrations through Lahijan registrar resale (WS-28).';
COMMENT ON COLUMN dns_domains.name           IS 'Canonical domain name with trailing dot; lowercase enforced by the service.';
COMMENT ON COLUMN dns_domains.status         IS 'Lifecycle status: available | registered | pending | transferred | expired.';
COMMENT ON COLUMN dns_domains.registrar_order_id IS 'Registrar back-office order id; cross-tenant unique when non-empty.';
COMMENT ON COLUMN dns_domains.zone_id        IS 'Optional FK into dns_zones; set when Lahijan auto-provisions the zone.';
COMMENT ON COLUMN dns_domains.price_cents    IS 'Frozen price snapshot at registration time; integer cents.';
COMMENT ON COLUMN dns_domains.ledger_entry_id IS 'Ledger row that paid for the registration; NULL for search results.';
COMMENT ON COLUMN dns_domains.is_dnssec_enabled IS 'Cached DNSSEC state; flipped by the registrar service when DS-at-parent + zone-sign both succeed.';
COMMENT ON COLUMN dns_domains.is_auto_renew  IS 'Off by default; when true the future renewal job charges + renews before expiry.';

-- Add a CHECK on status so a typo cannot strand a row in an unknown state.
ALTER TABLE dns_domains ADD CONSTRAINT dns_domains_status_valid
    CHECK (status IN ('available', 'registered', 'pending', 'transferred', 'expired'));

-- Add a CHECK on currency: 3-letter ISO 4217 shape. Multi-currency is
-- Phase 7 but the shape is enforced from day 1 so the migration is
-- additive (mirrors the billing tables from WS-17).
ALTER TABLE dns_domains ADD CONSTRAINT dns_domains_currency_valid
    CHECK (length(currency) = 3);

-- Add a CHECK on period_years: the ICANN-allowed range is 1..10 for
-- most TLDs; 10 is the safest upper bound for every registrar.
ALTER TABLE dns_domains ADD CONSTRAINT dns_domains_period_years_valid
    CHECK (period_years >= 1 AND period_years <= 10);
