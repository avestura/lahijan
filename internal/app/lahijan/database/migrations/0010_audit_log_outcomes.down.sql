-- Reverses 0010_audit_log_outcomes. Drops triggers + function explicitly so the
-- rollback is robust even if a partial run left state behind.
DROP TRIGGER IF EXISTS audit_log_outcomes_no_update ON audit_log_outcomes;
DROP TRIGGER IF EXISTS audit_log_outcomes_no_delete ON audit_log_outcomes;
DROP FUNCTION IF EXISTS audit_log_outcome_block_mutation();
DROP TABLE IF EXISTS audit_log_outcomes;
