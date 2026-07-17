-- 0008_email_tokens: single-use, expiring tokens for email verification,
-- password reset, and email change (WS-06). The raw token travels in a signed
-- link emailed to the user; only the SHA-256 hash is stored. used_at marks
-- single-use: once set, the token is rejected.
--
-- email_tokens is a global table: a user's verification/reset tokens are not
-- tied to any tenant (login is global).
CREATE TABLE email_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    kind        TEXT        NOT NULL,
    new_email   TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_email_tokens_token_hash ON email_tokens (token_hash);
CREATE INDEX        idx_email_tokens_user_id   ON email_tokens (user_id);
CREATE INDEX        idx_email_tokens_kind      ON email_tokens (kind);
CREATE INDEX        idx_email_tokens_expires   ON email_tokens (expires_at);

COMMENT ON TABLE  email_tokens              IS 'Single-use expiring tokens for verify-email, password-reset, and email-change.';
COMMENT ON COLUMN email_tokens.token_hash   IS 'SHA-256 hash of the raw token; raw token is never stored.';
COMMENT ON COLUMN email_tokens.kind         IS 'verify_email | password_reset | email_change.';
COMMENT ON COLUMN email_tokens.new_email    IS 'For email_change: the new email to apply on confirmation; NULL otherwise.';
COMMENT ON COLUMN email_tokens.used_at      IS 'Set when the token is consumed; single-use is enforced in the app layer.';
