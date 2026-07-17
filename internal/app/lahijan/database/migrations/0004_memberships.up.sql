-- 0004_memberships: the user ↔ tenant link, carrying the user's role within
-- that tenant. This is the canonical tenant-scoped table: it has tenant_id
-- NOT NULL and is the model every later tenant-scoped table copies.

CREATE TABLE memberships (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users (id)   ON DELETE CASCADE,
    role_id    UUID        REFERENCES roles (id)             ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

-- A user has exactly one membership per tenant (among non-deleted rows).
CREATE UNIQUE INDEX uq_memberships_tenant_user
    ON memberships (tenant_id, user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_memberships_tenant_id   ON memberships (tenant_id);
CREATE INDEX idx_memberships_user_id     ON memberships (user_id);
CREATE INDEX idx_memberships_role_id     ON memberships (role_id);
CREATE INDEX idx_memberships_deleted_at  ON memberships (deleted_at);

COMMENT ON TABLE  memberships           IS 'Tenant-scoped: links a user to a tenant with a role.';
COMMENT ON COLUMN memberships.tenant_id IS 'The tenant this membership belongs to; enforces row isolation.';
COMMENT ON COLUMN memberships.role_id   IS 'The role granted to the user within this tenant; nullable during bootstrap.';
