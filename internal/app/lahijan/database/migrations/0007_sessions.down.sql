-- Reverses 0007_sessions.
-- 0009_auth_columns.down drops refresh_tokens.session_id first, so the sessions
-- table has no remaining inbound FKs when we drop it here.
DROP TABLE IF EXISTS sessions;
