-- 0005_audit_log: append-only audit trail. tenant_id is nullable to record
-- system-level events that are not tied to any tenant (e.g. user login, admin
-- bootstrapping). Per ADR-0002 / pillar 7, audit rows cannot be UPDATEd or
-- DELETEd. Enforcement is via a BEFORE trigger that raises on any mutation,
-- so it applies even to the table owner (only a superuser with
-- session_replication_role=replica, or a DISABLE TRIGGER, can bypass it).

CREATE TABLE audit_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE NO ACTION,
    actor_user_id   UUID        REFERENCES users (id)   ON DELETE NO ACTION,
    actor_type      TEXT        NOT NULL DEFAULT 'user',
    action          TEXT        NOT NULL,
    resource_type   TEXT        NOT NULL,
    resource_id     UUID,
    status          TEXT        NOT NULL DEFAULT 'success',
    request_id      TEXT,
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_tenant_id     ON audit_log (tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_audit_log_actor_user_id ON audit_log (actor_user_id);
CREATE INDEX idx_audit_log_action        ON audit_log (action);
CREATE INDEX idx_audit_log_resource      ON audit_log (resource_type, resource_id);
CREATE INDEX idx_audit_log_created_at    ON audit_log (created_at);

COMMENT ON TABLE  audit_log              IS 'Append-only audit trail; UPDATE and DELETE are rejected by trigger.';
COMMENT ON COLUMN audit_log.tenant_id    IS 'The tenant context of the event; NULL for system-level events.';
COMMENT ON COLUMN audit_log.actor_type   IS 'Who performed the action: user, plugin, or system.';
COMMENT ON COLUMN audit_log.action       IS 'The privileged action, formatted scope.action.';
COMMENT ON COLUMN audit_log.status       IS 'success or failure of the audited action.';
COMMENT ON COLUMN audit_log.metadata     IS 'Arbitrary structured details; document schema in app layer.';

-- Block any UPDATE or DELETE on audit_log rows. The trigger raises with a
-- clear message so the integration test can assert on it.
CREATE OR REPLACE FUNCTION audit_log_block_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_block_mutation();

CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_block_mutation();
