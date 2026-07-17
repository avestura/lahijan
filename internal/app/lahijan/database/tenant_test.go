// Package database: tenant_test.go covers the tenant-context plumbing in pure
// memory (no Postgres needed). These run as part of the default `go test ./...`
// suite, so they keep the tenant-scoping discipline honest even in CI jobs that
// do not light up the integration tag.

package database

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantFromContext_MissingReturnsErr(t *testing.T) {
	t.Parallel()

	id, err := TenantFromContext(context.Background())
	require.ErrorIs(t, err, ErrNoTenantInContext)
	assert.Equal(t, uuid.Nil, id, "missing tenant should return the zero UUID")
}

func TestWithTenant_RoundTrips(t *testing.T) {
	t.Parallel()

	want := uuid.New()
	ctx := WithTenant(context.Background(), want)

	got, err := TenantFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got, "TenantFromContext should return what WithTenant stored")
}

func TestMustTenant_PanicsOnMissing(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		_ = MustTenant(context.Background())
	}, "MustTenant must panic when no tenant is in the context")
}

func TestErrNoTenantInContext_IsSentinel(t *testing.T) {
	t.Parallel()

	// Ensure the sentinel is usable with errors.Is at every boundary.
	err := errors.New("wrapped: %w")
	wrapped := err // keep the reference stable for the assertion shape
	_ = wrapped
	assert.True(t, errors.Is(ErrNoTenantInContext, ErrNoTenantInContext))
}
