-- 0050_directory_connections: external user/group directories (LDAP, SAML).
--
-- A platform administrator defines connections to an external directory. An
-- LDAP connection can be synced to import users and groups; a SAML connection
-- is an identity provider that signs users in and asserts their groups.
--
-- These are platform-level (global) tables like `users`: a connection is not
-- owned by a tenant, so there is no tenant_id column (see the Global row in
-- docs/glossary.md). Access is restricted to platform administrators by the
-- platform.directory.manage permission.

CREATE TABLE directory_connections (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ldap | saml.
    kind                TEXT        NOT NULL,
    -- Human-readable unique name shown in the admin UI. For SAML it is also
    -- the URL-safe provider key (see directory_connections_name_valid).
    name                TEXT        NOT NULL,
    enabled             BOOLEAN     NOT NULL DEFAULT TRUE,
    -- Non-secret, kind-specific settings (URLs, DNs, filters, attribute maps).
    config              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Secret material (LDAP bind password), AES-256-GCM sealed by auth/secrets.
    secret_encrypted    BYTEA,
    last_sync_at        TIMESTAMPTZ,
    -- ok | error; NULL until the first sync.
    last_sync_status    TEXT,
    last_sync_message   TEXT,
    last_sync_users     INTEGER,
    last_sync_groups    INTEGER,
    created_by          UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE directory_connections ADD CONSTRAINT directory_connections_kind_valid
    CHECK (kind IN ('ldap', 'saml'));
ALTER TABLE directory_connections ADD CONSTRAINT directory_connections_name_valid
    CHECK (name ~ '^[a-z0-9][a-z0-9_-]{0,62}$');
CREATE UNIQUE INDEX uq_directory_connections_name ON directory_connections (name);

-- Groups imported from a directory (LDAP sync) or learned from SAML assertions.
CREATE TABLE directory_groups (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id  UUID        NOT NULL REFERENCES directory_connections (id) ON DELETE CASCADE,
    -- Stable id in the directory (LDAP DN, or the SAML group claim value).
    external_id    TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    description    TEXT        NOT NULL DEFAULT '',
    synced_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_id, external_id)
);
CREATE INDEX idx_directory_groups_connection ON directory_groups (connection_id, name);

CREATE TABLE directory_group_members (
    group_id  UUID NOT NULL REFERENCES directory_groups (id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_directory_group_members_user ON directory_group_members (user_id);

-- Links a Lahijan user to its entry in a directory, so a re-sync updates the
-- same user instead of creating a duplicate, and so the UI can show where a
-- user came from.
CREATE TABLE directory_user_links (
    connection_id  UUID        NOT NULL REFERENCES directory_connections (id) ON DELETE CASCADE,
    user_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    external_id    TEXT        NOT NULL,
    synced_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, user_id),
    UNIQUE (connection_id, external_id)
);
CREATE INDEX idx_directory_user_links_user ON directory_user_links (user_id);
