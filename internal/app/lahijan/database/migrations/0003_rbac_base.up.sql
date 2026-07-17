-- 0003_rbac_base: roles, permissions, and role_permissions (all global).
-- RBAC policy enforcement ships in WS-08; this WS creates only the tables
-- and a seed set of base permissions so later WSs can reference them.

CREATE TABLE roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    is_system   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

-- slug is the stable machine identifier (e.g. "tenant.admin", "compute.operator").
CREATE UNIQUE INDEX uq_roles_slug        ON roles (slug) WHERE deleted_at IS NULL;
CREATE INDEX        idx_roles_deleted_at ON roles (deleted_at);

COMMENT ON TABLE roles             IS 'Named bundle of permissions; assigned to memberships.';
COMMENT ON COLUMN roles.slug       IS 'Stable machine identifier; unique among non-deleted roles.';
COMMENT ON COLUMN roles.is_system  IS 'TRUE for roles seeded by Lahijan that cannot be deleted.';

CREATE TABLE permissions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- slug follows "scope.action" (e.g. "compute.instance.create").
CREATE UNIQUE INDEX uq_permissions_slug ON permissions (slug);

COMMENT ON TABLE permissions       IS 'Atomic capability, formatted scope.action.';
COMMENT ON COLUMN permissions.slug IS 'Stable machine identifier; globally unique.';

CREATE TABLE role_permissions (
    role_id       UUID        NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_id UUID        NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_permission_id ON role_permissions (permission_id);

COMMENT ON TABLE role_permissions IS 'Many-to-many link between roles and permissions.';
