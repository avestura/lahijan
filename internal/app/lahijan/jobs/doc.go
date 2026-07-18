// Package jobs is Lahijan's durable-job-queue layer (WS-09): it wraps
// github.com/riverqueue/river (chosen in ADR-0008) so that any other module
// can queue durable work that survives restarts, retries on failure, and is
// inspectable from the admin UI.
//
// # Architecture
//
// The package is organised around five concerns:
//
//   - Client  — the River client, built once at bootstrap on top of the
//     existing pgxpool and shared by every producer (services that queue
//     work) and consumer (workers that execute it).
//   - Supervisor — graceful start/stop; sits next to the Fiber server in
//     program.Start so the whole process shuts down cleanly on SIGTERM.
//   - Registry — the central registry of job kinds -> worker functions.
//     Every worker that runs in this process is registered here at startup;
//     the River client cross-checks inserted jobs against it.
//   - Middleware — an OpenTelemetry worker middleware that emits a span and
//     bumps a per-kind metric on every execution (pillar 12 / ADR-0016).
//   - Examples — small no-op workers for the four WS-09 example kinds
//     (billing.usage.rollup, auditlog.prune, notify.email.send,
//     compute.instance.snapshot) so the system is exercisable end-to-end
//     before the real domain modules land.
//
// # Layering
//
// The jobs package depends on the database package (for the pool) and is
// itself consumed by:
//
//   - api/admin_jobs_handlers.go (the admin API for inspecting + retrying +
//     cancelling jobs); and
//   - every domain module that needs durable work (compute start/stop, DNS
//     zone propagation, snapshot scheduling, metering rollup, ...).
//
// It never imports the api or domain packages.
//
// # Multi-node safety
//
// River is PostgreSQL-native: every Lahijan replica that connects to the same
// `lahijan` database competes for the same river_job rows via row-level
// locking. There is no leader election requirement for *workers*; the
// maintenance services (queue cleaner, rescuer, scheduler) do elect a leader
// via the river_leader table, but that is internal to River. This satisfies
// ADR-0009 (multi-node-ready from day 1): running two Lahijan replicas
// against the same Postgres is already safe for both the API and the queue.
//
// # Tenant scoping
//
// River's tables are GLOBAL per the glossary (no tenant_id). Tenant scoping
// for a job is encoded in the job's args (the inserting module is expected to
// carry tenant_id in its JobArgs struct); the worker reads it back and uses
// database.WithTenant before any DB call. The admin API exposes jobs across
// all tenants because platform.admin is itself a global role.
package jobs
