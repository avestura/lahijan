-- 0006_tokens: refresh_tokens and personal_access_tokens (both global).
-- Lahijan issues refresh tokens for web sessions and personal access tokens
-- for API clients. Auth issuance/rotation lands in WS-06; this WS creates the
-- storage tables only.

CREATE TABLE refresh_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent  TEXT,
    ip_address  INET
);

-- token_hash is what we actually store (the raw token never lives in the DB).
CREATE UNIQUE INDEX uq_refresh_tokens_token_hash ON refresh_tokens (token_hash);
CREATE INDEX        idx_refresh_tokens_user_id   ON refresh_tokens (user_id);
CREATE INDEX        idx_refresh_tokens_expires   ON refresh_tokens (expires_at);

COMMENT ON TABLE  refresh_tokens             IS 'Long-lived session refresh tokens; global, one user can have many.';
COMMENT ON COLUMN refresh_tokens.token_hash  IS 'SHA-256 hash of the raw refresh token; raw token is never stored.';
COMMENT ON COLUMN refresh_tokens.revoked_at  IS 'Set when the token is manually revoked; NULL means still valid.';

CREATE TABLE personal_access_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    token_hash  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_pat_token_hash   ON personal_access_tokens (token_hash);
CREATE INDEX        idx_pat_user_id     ON personal_access_tokens (user_id);

COMMENT ON TABLE  personal_access_tokens IS 'User-issued API tokens; global, one user can have many.';
COMMENT ON COLUMN personal_access_tokens.token_hash IS 'SHA-256 hash of the raw PAT; the raw token is shown only once at creation.';
