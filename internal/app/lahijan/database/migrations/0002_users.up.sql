-- 0002_users: a person who can log in (global table). password_hash is
-- nullable to support OAuth-only / SSO-only users (ADR-0004).

CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT        NOT NULL,
    password_hash TEXT,
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

-- email is unique across the platform: one identity per email.
CREATE UNIQUE INDEX uq_users_email          ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX        idx_users_active        ON users (is_active);
CREATE INDEX        idx_users_deleted_at    ON users (deleted_at);

COMMENT ON TABLE  users              IS 'A person who can log in; global, not tenant-scoped.';
COMMENT ON COLUMN users.email        IS 'Unique identity; case-insensitive matching done in app layer.';
COMMENT ON COLUMN users.password_hash IS 'argon2id hash; NULL for OAuth/SSO-only users.';
