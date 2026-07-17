// Package database: tenant.go provides the tenant-context plumbing that every
// tenant-scoped repository uses. Per ADR-0002, tenant isolation is enforced at
// the repository layer: middleware sets the tenant id into the request context,
// and each tenant-scoped repo extracts it via TenantFromContext before running
// any query. This file is the ONLY place allowed to read the tenant from a
// context.
package database

import (
	"context"

	"github.com/google/uuid"
)

type tenantCtxKey struct{}

// WithTenant returns a derived context that carries the given tenant id. The
// HTTP tenant middleware (WS-05) calls this once per request after auth, then
// passes the enriched context down to services and repositories. Tests use the
// same helper to set up a tenant scope.
func WithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, tenantID)
}

// TenantFromContext returns the tenant id baked into ctx, or
// ErrNoTenantInContext if none is present. Tenant-scoped repositories MUST call
// this and pass the result as the first query parameter; they never accept a
// tenant id from the caller directly.
func TenantFromContext(ctx context.Context) (uuid.UUID, error) {
	v, ok := ctx.Value(tenantCtxKey{}).(uuid.UUID)
	if !ok {
		return uuid.Nil, ErrNoTenantInContext
	}
	return v, nil
}

// MustTenant is the convenience form of TenantFromContext for use inside repos
// that have already validated the context (it panics on a missing tenant, which
// indicates a programmer error rather than a runtime condition).
func MustTenant(ctx context.Context) uuid.UUID {
	t, err := TenantFromContext(ctx)
	if err != nil {
		panic(err)
	}
	return t
}
