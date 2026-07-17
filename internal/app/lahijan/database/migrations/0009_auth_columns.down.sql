-- Reverses 0009_auth_columns. Drop added columns and their indexes. The order
-- (PAT, then refresh_tokens, then users) keeps FK dependencies intact.
ALTER TABLE personal_access_tokens DROP COLUMN IF EXISTS scopes;

DROP INDEX IF EXISTS idx_refresh_tokens_family_id;
DROP INDEX IF EXISTS idx_refresh_tokens_session_id;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS family_id;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS session_id;

ALTER TABLE users DROP COLUMN IF EXISTS locale;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS display_name;
