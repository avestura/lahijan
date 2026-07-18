-- 0014_user_webauthn_credentials: per-user WebAuthn / passkey credentials
-- (WS-07c). Global (no tenant_id) for the same reason user_totp_secrets is
-- global: a credential is bound to a user, not to any tenant, and protects
-- every login regardless of membership.
--
-- The public_key column holds the COSE-encoded credential public key as
-- returned by the authenticator at registration time. Per the W3C WebAuthn
-- spec, the public key is NOT sensitive (authenticators sign with the
-- matching private key, never reveal it); so unlike user_totp_secrets we do
-- not encrypt it. The credential_id is the unpadded base64url of the
-- authenticator's identifier for this credential.
--
-- A user can have many credentials (one Touch ID + one YubiKey + one
-- backup). The sign_count column carries the WebAuthn replay-detection
-- counter: each assertion must carry a count strictly greater than the last
-- stored value (or, for authenticators that don't support counts, equal).
-- Transports is the list of UI hints the browser gave at registration time
-- (usb, nfc, ble, internal, hybrid, smart-card) used to pick the right
-- transport at login time.

CREATE TABLE user_webauthn_credentials (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The WebAuthn credential id (unpadded base64url). Unique per
    -- (user, credential_id) so the same credential cannot be registered
    -- twice against the same user; cross-user collisions are vanishingly
    -- rare (the credential id is a 16+ byte random) and handled by the
    -- unique index below.
    credential_id   TEXT        NOT NULL,
    -- The COSE-encoded public key blob (raw bytes, base64). The RP verifies
    -- signatures against this key on every assertion. Not sensitive per the
    -- W3C WebAuthn spec; stored as-is.
    public_key      BYTEA       NOT NULL,
    -- The COSE algorithm identifier (e.g. -7 = ES256, -257 = RS256). Used by
    -- the WebAuthn library to pick the right verifier at assertion time.
    aaguid          UUID,
    -- The WebAuthn sign counter for replay detection. Authenticators that
    -- support it MUST bump the count on every assertion; the RP rejects any
    -- assertion whose count is not strictly greater than the stored value.
    -- Authenticators that don't support counts always send 0; the RP
    -- accepts that as a no-op (cannot detect replay).
    sign_count      BIGINT      NOT NULL DEFAULT 0,
    -- Bit-encoded transports list (the W3C spec carries these as a string
    -- array). Stored as a TEXT[] for clarity: {usb, nfc, ble, internal,
    -- hybrid, smart-card}. Empty array when the browser did not report any.
    transports      TEXT[]      NOT NULL DEFAULT '{}',
    -- User-supplied label ("MacBook Touch ID", "YubiKey 5C NFC"). Helps the
    -- user identify which credential to revoke later.
    name            TEXT        NOT NULL DEFAULT '',
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_user_webauthn_credentials_user_credential_id
    ON user_webauthn_credentials (user_id, credential_id);
CREATE INDEX idx_user_webauthn_credentials_user_id
    ON user_webauthn_credentials (user_id);

COMMENT ON TABLE  user_webauthn_credentials                       IS 'Per-user WebAuthn / passkey credentials; global, public_key not encrypted (not sensitive per spec).';
COMMENT ON COLUMN user_webauthn_credentials.credential_id         IS 'Unpadded base64url of the credential id from the authenticator.';
COMMENT ON COLUMN user_webauthn_credentials.public_key            IS 'COSE-encoded public key blob (raw bytes).';
COMMENT ON COLUMN user_webauthn_credentials.sign_count            IS 'WebAuthn replay-detection counter; bumped on every assertion.';
COMMENT ON COLUMN user_webauthn_credentials.transports            IS 'UI hints the browser reported at registration: usb, nfc, ble, internal, hybrid, smart-card.';
COMMENT ON COLUMN user_webauthn_credentials.name                  IS 'User-supplied label for the credential.';
