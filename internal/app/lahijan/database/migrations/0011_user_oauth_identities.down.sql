-- 0011_user_oauth_identities (down): reversible drop of the linking table.
-- No data migration is needed because the table starts empty; a re-up recreates it.

DROP TABLE IF EXISTS user_oauth_identities;
