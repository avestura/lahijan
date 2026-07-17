-- 0007_sessions: a logical login session (WS-06). The browser carries an opaque
-- signed cookie whose SHA-256 hash matches sessions.token_hash. refresh_tokens
-- (migration 0009) link back here via session_id for rotation + reuse detection.
--
-- sessions is a global table: a user has sessions independent of any tenant.
CREATE TABLE sessions (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent   TEXT,
    ip_address   INET
);

-- token_hash is what we actually store; the raw cookie secret is never stored.
CREATE UNIQUE INDEX uq_sessions_token_hash ON sessions (token_hash);
CREATE INDEX        idx_sessions_user_id    ON sessions (user_id);
CREATE INDEX        idx_sessions_expires    ON sessions (expires_at);

COMMENT ON TABLE  sessions             IS 'A logical login session; global, backed by an opaque signed cookie.';
COMMENT ON COLUMN sessions.token_hash  IS 'SHA-256 hash of the cookie secret; raw secret is never stored.';
COMMENT ON COLUMN sessions.revoked_at  IS 'Set when the session is logged out; NULL means still valid.';
COMMENT ON COLUMN sessions.last_seen_at IS 'Updated on each authenticated request that uses this session.';
