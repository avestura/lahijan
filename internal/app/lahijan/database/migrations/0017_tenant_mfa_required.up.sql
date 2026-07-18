-- 0017_tenant_mfa_required: adds a per-tenant MFA-required policy column
-- (WS-07c). When TRUE, every member of the tenant MUST have at least one
-- MFA factor enrolled to log in. Enforcement happens at login:
--
--   - After the password (or external-IdP) step succeeds, the session
--     service loads every membership row the user has, joins each to its
--     tenant, and if ANY tenant has mfa_required = TRUE, the login flow
--     issues a pending_session_token instead of a real session.
--   - The user must then complete the MFA challenge (TOTP / WebAuthn /
--     recovery) before the real session is issued.
--   - If the user has no MFA factor enrolled AND any membership's tenant
--     requires MFA, the login is rejected with a "complete MFA enrollment
--     required" envelope. The dashboard would prompt for enrollment on
--     the next visit.
--
-- A column on tenants (rather than a separate tenant_settings table)
-- keeps the data model small for MVP. A future WS can lift it into a
-- dedicated settings table if the column count grows.

ALTER TABLE tenants
    ADD COLUMN mfa_required BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN tenants.mfa_required IS 'When TRUE, every member of this tenant must complete an MFA challenge at login.';
