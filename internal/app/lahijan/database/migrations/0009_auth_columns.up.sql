-- 0009_auth_columns: extra columns on users, refresh_tokens, and
-- personal_access_tokens for WS-06. Each addition is independently reversible;
-- no data migration is needed because the auth tables start empty.
--
--   users.display_name, users.email_verified_at, users.locale
--   refresh_tokens.session_id (FK sessions), refresh_tokens.family_id
--   personal_access_tokens.scopes

ALTER TABLE users
    ADD COLUMN display_name      TEXT,
    ADD COLUMN email_verified_at TIMESTAMPTZ,
    ADD COLUMN locale            TEXT NOT NULL DEFAULT 'en';

ALTER TABLE refresh_tokens
    ADD COLUMN session_id UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    ADD COLUMN family_id  UUID NOT NULL;

CREATE INDEX idx_refresh_tokens_session_id ON refresh_tokens (session_id);
CREATE INDEX idx_refresh_tokens_family_id  ON refresh_tokens (family_id);

ALTER TABLE personal_access_tokens
    ADD COLUMN scopes TEXT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN users.display_name       IS 'Optional human-friendly display name.';
COMMENT ON COLUMN users.email_verified_at  IS 'When the user verified their email; NULL until verified.';
COMMENT ON COLUMN users.locale             IS 'Preferred locale code (en, fa, ...). Default en.';
COMMENT ON COLUMN refresh_tokens.session_id IS 'The session this refresh token belongs to (WS-06).';
COMMENT ON COLUMN refresh_tokens.family_id  IS 'Rotation family: reusing any rotated token revokes the whole family + the session.';
COMMENT ON COLUMN personal_access_tokens.scopes IS 'Permission slugs the PAT grants; enforced by RequirePerm in WS-08.';
