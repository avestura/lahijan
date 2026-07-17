# ADR-0019: Append-only audit outcome trail (MarkOutcome pattern)

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-08 needs both an immutable audit log (pillar 7) AND a `MarkOutcome` seam
that records the result of a privileged action *after* the action completes.
A typical flow is:

```
auditID, _ := emitter.Emit(ctx, Event{Action: "compute.instance.create", Status: "pending"})
defer func() { _ = emitter.MarkOutcome(ctx, auditID, status, details) }()
// ... perform the privileged action ...
```

Migration 0005 (WS-03) already makes `audit_log` strictly append-only: a
BEFORE trigger rejects every UPDATE and DELETE. That trigger is the
tamper-evidence guarantee pillar 7 calls for.

Options considered for the MarkOutcome seam:

- **Option A — Allow narrow UPDATEs on audit_log.** Replace the
  unconditional trigger with one that permits UPDATE only on `(status,
  metadata, updated_at)`. Simple; keeps everything in one table. **Con:**
  violates the WS-08 DoD item "audit table rejects UPDATE and DELETE".
  Also weakens the tamper-evidence story (a future migration that loosens
  the trigger further could go unnoticed).
- **Option B — Sibling `audit_log_outcomes` table, also append-only.** The
  initial emit creates the immutable `audit_log` row; each MarkOutcome call
  appends a new row to `audit_log_outcomes`. The "current status" of an
  event is the latest outcome's status. Same tamper-evidence contract as
  `audit_log`. **Con:** two tables to reason about.
- **Option C — Outbox pattern with deferred writes.** MarkOutcome enqueues
  an outcome row via River; a worker drains and writes. **Con:** eventual
  consistency; out-of-order writes possible; adds a River dependency to a
  primitive that should be synchronous.

## Decision

Lahijan uses **Option B**: a sibling `audit_log_outcomes` table (migration
0010) with the same append-only trigger contract as `audit_log`. The
MarkOutcome seam (`audit.Emitter.MarkOutcome`) writes a row there; nothing
ever updates or deletes from either table.

The "current status" of an audit event is the latest outcome row's status,
falling back to the `audit_log.status` column when no outcome rows exist
(backwards-compatible with the WS-06 fire-and-forget emit pattern that
supplies the final status up front).

## Consequences

- **Positive:** both tables share the same tamper-evidence contract
  (trigger-rejected UPDATE/DELETE); one mental model.
- **Positive:** MarkOutcome is idempotent in the sense that multiple calls
  for the same audit id produce a visible trail (useful for long-running
  privileged actions that emit pending -> in-progress -> success).
- **Positive:** the audit query API can render the outcome trail as a
  timeline without consulting a separate system.
- **Negative:** one extra join (or one extra query) when reading the current
  status. Mitigated by an index on `(audit_id, created_at DESC)`.
- **Negative:** schemas and migrations get slightly larger. Acceptable for
  the audit subsystem.

## Compliance

- `audit_log` and `audit_log_outcomes` both have BEFORE UPDATE and BEFORE
  DELETE triggers (`audit_log_block_mutation` and
  `audit_log_outcome_block_mutation` in migrations 0005 and 0010).
- `audit.Emitter.Emit` returns the new audit row's id; the caller passes
  it to `MarkOutcome`.
- `audit.DBEmitter.MarkOutcome` writes to `audit_log_outcomes` via
  `AuditLogRepository.MarkOutcome`; never touches `audit_log`.
- The audit query API (`/api/v1/audit`, `/api/v1/audit/{id}`) renders the
  outcome trail newest-first; the "current status" is the first element.

## References

- WS-08 doc (`docs/workstreams/WS-08-rbac-audit-log.md`)
- ADR-0002 (Multi-tenant row-level isolation — audit_log is the exception
  with a nullable tenant_id for system-level events)
