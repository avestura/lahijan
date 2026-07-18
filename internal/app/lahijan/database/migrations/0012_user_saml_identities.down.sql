-- 0012_user_saml_identities (down): reversible drop of the SAML linking table.
-- No data migration is needed because the table starts empty; a re-up recreates it.

DROP TABLE IF EXISTS user_saml_identities;
