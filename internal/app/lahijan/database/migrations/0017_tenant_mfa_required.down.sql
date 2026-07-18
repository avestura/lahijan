-- Reverse 0017: drop the mfa_required column from tenants.
ALTER TABLE tenants DROP COLUMN IF EXISTS mfa_required;
