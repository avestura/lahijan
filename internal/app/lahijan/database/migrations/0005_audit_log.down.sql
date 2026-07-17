-- Reverses 0005_audit_log. DROP TRIGGER + DROP FUNCTION is belt-and-suspenders;
-- DROP TABLE alone would cascade, but being explicit makes the rollback robust
-- even if the function survives a partial run.
DROP TRIGGER IF EXISTS audit_log_no_update ON audit_log;
DROP TRIGGER IF EXISTS audit_log_no_delete ON audit_log;
DROP FUNCTION IF EXISTS audit_log_block_mutation();
DROP TABLE IF EXISTS audit_log;
