// Package compute: advisory_lock_test.go covers the WS-26 Postgres
// advisory-lock helpers. The actual lock acquisition needs a live
// Postgres (covered by the integration suite); these unit tests cover
// the deterministic bits (hash, noop backend).
package compute

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTenantHash_StableForSameInput asserts the hash function is
// stable. A tenant that maps to a given lock key today must map to
// the same key tomorrow so the per-tenant serialisation still works
// across restarts.
func TestTenantHash_StableForSameInput(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("12345678-1234-1234-1234-1234567890ab")
	h1 := tenantHash(id)
	h2 := tenantHash(id)
	assert.Equal(t, h1, h2)
}

// TestTenantHash_DiffersForDifferentInputs asserts two distinct
// tenants get distinct hash keys (collisions are tolerable but
// unlikely; we just guard against a degenerate always-same hash).
func TestTenantHash_DiffersForDifferentInputs(t *testing.T) {
	t.Parallel()
	id1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	id2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	assert.NotEqual(t, tenantHash(id1), tenantHash(id2))
}

// TestNoopAdvisoryLocker_AlwaysSucceeds asserts the no-op backend
// (used in unit tests + when the pool is nil) returns a usable
// release func.
func TestNoopAdvisoryLocker_AlwaysSucceeds(t *testing.T) {
	t.Parallel()
	l := noopAdvisoryLocker{}
	release, err := l.LockTenant(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.NotPanics(t, func() { release() })
}

// TestNewPGAdvisoryLocker_NilPoolFallsBack asserts the constructor
// degrades cleanly when the pool is nil (e.g. dev without compute
// wired).
func TestNewPGAdvisoryLocker_NilPoolFallsBack(t *testing.T) {
	t.Parallel()
	l := NewPGAdvisoryLocker(nil)
	_, ok := l.(noopAdvisoryLocker)
	assert.True(t, ok, "nil pool should produce a noopAdvisoryLocker")
}
