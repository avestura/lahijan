-- Reverse 0013: drop user_totp_secrets. No data to preserve in a rollback
-- (the user re-enrolls after re-applying the up migration).
DROP TABLE IF EXISTS user_totp_secrets;
