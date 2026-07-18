-- 0015_user_recovery_codes: per-user single-use recovery codes (WS-07c).
-- Each user gets a fixed batch of 10 codes that can be used to bypass MFA
-- once each in case the user loses their TOTP device / YubiKey. The codes
-- are SHA-256 hashed at rest — the raw code is shown to the user exactly
-- once, at generation time, and never stored.
--
-- The table is global (no tenant_id) for the same reason every other
-- credential table is global: a recovery code protects the user's identity,
-- not any single tenant membership.
--
-- code_hash holds the lowercase hex SHA-256 of the raw code (the raw code
-- is never stored). The lookup path is: SHA-256 the user-supplied code,
-- find a matching row scoped by (user_id, hash); on hit, mark used_at.
-- Single-use is enforced by the (user_id, code_hash) unique index AND by
-- the used_at IS NULL filter at lookup time.

CREATE TABLE user_recovery_codes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 hex digest of the raw code. The raw code is never stored.
    code_hash  TEXT        NOT NULL,
    -- NULL until the code is consumed by a successful recovery challenge.
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One code hash per user: prevents the same raw code from being inserted
-- twice under the same user. (Two users can theoretically share a hash
-- if their codes collided, but the SHA-256 collision probability on a
-- 10-byte space is negligible.)
CREATE UNIQUE INDEX uq_user_recovery_codes_user_code_hash
    ON user_recovery_codes (user_id, code_hash);
CREATE INDEX idx_user_recovery_codes_user_id
    ON user_recovery_codes (user_id);

COMMENT ON TABLE  user_recovery_codes               IS 'Per-user single-use recovery codes; global, SHA-256 hashed at rest (raw code never stored).';
COMMENT ON COLUMN user_recovery_codes.code_hash     IS 'SHA-256 hex digest of the raw recovery code.';
COMMENT ON COLUMN user_recovery_codes.used_at       IS 'NULL until consumed by a successful recovery challenge; one-shot.';
