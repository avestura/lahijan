-- 0016_mfa_pending_sessions: short-lived, single-use "pending session"
-- tokens issued during login when the user has MFA enabled (WS-07c). After
-- the password (or external-IdP) step succeeds, the session service issues
-- a pending_session_token (NOT a real session) that the user exchanges at
-- /api/v1/auth/mfa/challenge once they pass the MFA challenge. Only on
-- success does the real session + refresh token get issued.
--
-- Design:
--   - The pending token is opaque + signed via auth/secrets.Signer, just
--     like refresh tokens; we store the SHA-256 hash here and lookup by hash.
--   - TTL is short (default 5 minutes; configurable via
--     auth.mfa.pendingTTLSeconds).
--   - Single-use: consumed_at is set when the challenge succeeds. A second
--     attempt with the same token is rejected.
--   - Failure tracking: failed_attempts counts consecutive failed
--     challenges; at 5 (configurable) the pending session is revoked and
--     the user must log in again from scratch. This blocks brute-force
--     attacks on the 6-digit TOTP code.
--
-- Global table: the pending session pre-dates any tenant selection — the
-- user is mid-login and the membership context has not been chosen yet.

CREATE TABLE mfa_pending_sessions (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 hash of the raw pending token (raw token is never stored).
    token_hash       TEXT        NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    -- NULL until consumed by a successful MFA challenge. Single-use is
    -- enforced by checking this in the lookup query.
    consumed_at      TIMESTAMPTZ,
    -- Set when the pending session has been burned (revoked, expired, or
    -- brute-force locked out).
    revoked_at       TIMESTAMPTZ,
    -- Number of consecutive failed MFA challenges against this token. At
    -- max_failed_attempts (configurable) the row is auto-revoked.
    failed_attempts  INT         NOT NULL DEFAULT 0,
    -- The client metadata captured at issue time, propagated to the real
    -- session when the challenge succeeds.
    user_agent       TEXT,
    ip_address       INET,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_mfa_pending_sessions_token_hash ON mfa_pending_sessions (token_hash);
CREATE INDEX        idx_mfa_pending_sessions_user_id   ON mfa_pending_sessions (user_id);
CREATE INDEX        idx_mfa_pending_sessions_expires   ON mfa_pending_sessions (expires_at);

COMMENT ON TABLE  mfa_pending_sessions                  IS 'Short-lived, single-use pending session tokens issued during login when MFA is required; global.';
COMMENT ON COLUMN mfa_pending_sessions.token_hash       IS 'SHA-256 hash of the raw pending token; raw token is never stored.';
COMMENT ON COLUMN mfa_pending_sessions.consumed_at      IS 'NULL until consumed by a successful MFA challenge; single-use.';
COMMENT ON COLUMN mfa_pending_sessions.failed_attempts  IS 'Number of consecutive failed challenges; at 5 the row is auto-revoked.';
