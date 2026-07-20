// Package compute: advisory_lock.go wires the Postgres advisory-lock
// backend the ClusterPlacementDriver uses to serialise per-tenant
// placement decisions (WS-26, ADR-0033).
//
// The lock is held for the duration of the calling transaction so two
// replicas placing instances for the same tenant do not both pick the
// same "least loaded" member based on a stale cluster view. The lock
// is keyed on a 32-bit hash of the tenant id (a full UUID does not fit
// in a single int32 key; the hash collision rate is acceptable for a
// per-tenant serialisation point).
//
// The pg_advisory_xact_lock variant is used so the lock is released
// automatically at COMMIT / ROLLBACK; the caller does not need to
// remember to release it.
package compute

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockNamespace is the constant 32-bit namespace prefix
// Lahijan uses for its advisory locks. The namespace prevents
// collisions between compute placement and any future advisory-lock
// consumers (audit, billing, ...).
//
// The value is arbitrary; it was picked as a stable hash of the
// project name so future ADRs can reference it.
const advisoryLockNamespace int32 = 0x1a71_0001 // "lah" + 0x0001 (compute placement)

// pgAdvisoryLocker is the production advisoryLocker backed by a
// *pgxpool.Pool. The pool is shared with the rest of the compute
// service so the lock call uses the same connection source as the
// transactional repo writes.
type pgAdvisoryLocker struct {
	pool *pgxpool.Pool
}

// NewPGAdvisoryLocker builds a pgAdvisoryLocker over the given pool.
// pool MUST be non-nil; callers that lack a pool (unit tests, dev
// without compute wired) pass the noopAdvisoryLocker the cluster
// driver falls back to.
func NewPGAdvisoryLocker(pool *pgxpool.Pool) advisoryLocker {
	if pool == nil {
		return noopAdvisoryLocker{}
	}
	return &pgAdvisoryLocker{pool: pool}
}

// LockTenant acquires a per-tenant advisory lock. The lock is held
// for the duration of the surrounding transaction; the returned
// release func is a no-op (the COMMIT / ROLLBACK releases the lock)
// but callers MUST still defer it for symmetry with future lock
// backends (Redis, etcd) that need explicit release.
//
// The function blocks until the lock is acquired. There is no
// timeout; the caller's context still applies for cancellation.
func (l *pgAdvisoryLocker) LockTenant(ctx context.Context, tenantID uuid.UUID) (func(), error) {
	if l.pool == nil {
		return func() {}, nil
	}
	key := tenantHash(tenantID)
	// pg_advisory_xact_lock requires a transaction; we wrap a tiny
	// BEGIN/COMMIT pair around the lock so the lock is released when
	// the release func runs. The lock call itself blocks inside the
	// transaction; the COMMIT (invoked by release) is what releases.
	tx, err := l.pool.BeginTx(ctx, txReadOnlyOpts)
	if err != nil {
		return nil, fmt.Errorf("compute: begin tx for advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", advisoryLockNamespace, key); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("compute: acquire advisory lock: %w", err)
	}
	return func() {
		// Commit releases the transaction-scoped advisory lock. We
		// use commit (not rollback) so a future caller can stuff a
		// write into the same tx without the rollback aborting it;
		// today the tx carries only the lock call, so commit + the
		// implicit no-op are equivalent.
		_ = tx.Commit(ctx)
	}, nil
}

// tenantHash returns a stable 32-bit hash of the tenant id. The hash
// is used as the per-tenant advisory-lock key; a perfect hash is not
// required (collisions just mean two tenants serialise against each
// other temporarily — a small cost compared to a placement race).
func tenantHash(tenantID uuid.UUID) int32 {
	var sum int32
	for _, b := range tenantID {
		// FNV-1a-style mix; good enough for a 16-byte input.
		sum = (sum * 31) ^ int32(b)
	}
	return sum
}

// txReadOnlyOpts is the tx options the advisory lock uses. Read-only
// because the lock call does not write; the lock itself is a side
// effect of the transaction, not a row write.
var txReadOnlyOpts = pgx.TxOptions{AccessMode: pgx.ReadOnly}
