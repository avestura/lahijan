-- 0010_audit_log_outcomes: the MarkOutcome seam for the audit emit pipeline
-- (WS-08). audit_log itself is fully append-only (migration 0005 blocks every
-- UPDATE and DELETE), so status transitions for a privileged action are recorded
-- here, in a sibling append-only table. A typical emit flow is:
--
--   1. Emit(ctx, event with status=pending) -> audit_log row (immutable).
--   2. The privileged action runs.
--   3. MarkOutcome(ctx, auditID, status=success|failure, details) -> row here.
--
-- The "current status" of an audit event is the latest outcome row's status, or
-- audit_log.status when no outcome rows exist yet (backwards compatible with
-- WS-06 which only ever emitted the final status).
--
-- This keeps the immutable "what happened" record (audit_log) separate from
-- the equally immutable "what was the result" trail (audit_log_outcomes), and
-- preserves the pillar-7 immutability guarantee that every audit row is
-- permanent.

CREATE TABLE audit_log_outcomes (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    audit_id   UUID        NOT NULL REFERENCES audit_log (id) ON DELETE NO ACTION,
    status     TEXT        NOT NULL,
    details    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot read path: listing outcomes for one audit event (newest first) and the
-- reverse lookup (audit rows that have at least one outcome of a status).
CREATE INDEX idx_audit_log_outcomes_audit_id ON audit_log_outcomes (audit_id, created_at DESC);
CREATE INDEX idx_audit_log_outcomes_status   ON audit_log_outcomes (status);

COMMENT ON TABLE  audit_log_outcomes         IS 'Append-only outcome trail for an audit_log row; UPDATE and DELETE are rejected by trigger.';
COMMENT ON COLUMN audit_log_outcomes.audit_id IS 'The audit_log row this outcome belongs to.';
COMMENT ON COLUMN audit_log_outcomes.status   IS 'success | failure | pending (the new current status of the audit event).';
COMMENT ON COLUMN audit_log_outcomes.details  IS 'Optional structured details about the outcome (e.g. error message).';

-- Block any UPDATE or DELETE so the outcome trail is tamper-evident, just like
-- audit_log. Reuses the same exception shape so callers see the same contract.
CREATE OR REPLACE FUNCTION audit_log_outcome_block_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_log_outcomes is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER audit_log_outcomes_no_update BEFORE UPDATE ON audit_log_outcomes
    FOR EACH ROW EXECUTE FUNCTION audit_log_outcome_block_mutation();

CREATE TRIGGER audit_log_outcomes_no_delete BEFORE DELETE ON audit_log_outcomes
    FOR EACH ROW EXECUTE FUNCTION audit_log_outcome_block_mutation();
