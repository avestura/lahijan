-- 0013_user_totp_secrets: a per-user TOTP (RFC 6238) secret used as a second
-- factor during login (WS-07c). The table is global (no tenant_id) because a
-- user authenticates against the platform, not against any one tenant, and the
-- same TOTP code protects every login regardless of the membership the user is
-- about to exercise.
--
-- The secret column holds AES-GCM ciphertext (auth/secrets.Crypto): a TOTP
-- secret is high-sensitivity material — exfiltrating it lets an attacker
-- mint 6-digit codes from anywhere. Storing the raw base32 secret would let
-- anyone with DB read access bypass the second factor. The wire format is
-- base64(nonce || ciphertext || tag) produced by auth/secrets.Crypto.Seal.
--
-- One row per user (enforced by the unique index on user_id). A user with no
-- TOTP enrollment has no row; a user with row but confirmed_at IS NULL has
-- begun enrollment (the secret was generated and is awaiting the verification
-- 6-digit code from their authenticator app).

CREATE TABLE user_totp_secrets (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    -- AES-GCM ciphertext of the base32-encoded TOTP secret. Encoded by
    -- auth/secrets.Crypto.Seal (base64(nonce || ciphertext || tag)).
    secret       TEXT        NOT NULL,
    -- NULL until the user completes enrollment by submitting a valid 6-digit
    -- code from their authenticator app. Until confirmed, the secret does
    -- NOT count as an enrolled second factor — login skips the MFA challenge.
    confirmed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE  user_totp_secrets             IS 'Per-user TOTP secret; global, AES-GCM encrypted at rest. One row per user; confirmed_at IS NULL means enrollment is pending.';
COMMENT ON COLUMN user_totp_secrets.secret       IS 'AES-GCM ciphertext (base64) of the base32 TOTP secret.';
COMMENT ON COLUMN user_totp_secrets.confirmed_at IS 'NULL until the user verifies a 6-digit code; until then login does not challenge for TOTP.';
