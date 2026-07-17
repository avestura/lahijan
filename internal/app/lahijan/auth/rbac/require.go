// Package rbac: require.go is the service-layer helper for code paths that
// need to enforce a permission outside of an HTTP handler. The HTTP path
// goes through api/middleware.RequirePerm; this is for jobs, websockets, and
// any other entry point that resolves a principal manually.
package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrPermissionDenied is returned by Require when the caller does not hold
// the requested permission. Translate it to the appropriate transport-level
// error (e.g. HTTP 403) at the boundary.
var ErrPermissionDenied = errors.New("rbac: permission denied")

// ErrTenantScopeRequired is returned by Require when the caller did not supply
// a tenant id. Distinct from ErrPermissionDenied so the boundary can render
// the right status (400 vs 403).
var ErrTenantScopeRequired = errors.New("rbac: tenant scope required")

// ErrUnauthenticated is returned by Require when the caller did not supply a
// user id.
var ErrUnauthenticated = errors.New("rbac: user id required")

// Require is the service-layer analogue of api/middleware.RequirePerm. It
// runs the same three checks (user -> tenant -> policy) and returns a typed
// sentinel on each failure so the caller can branch.
//
// Callers MUST propagate the returned error verbatim; wrapping it would hide
// the sentinel from errors.Is at the boundary. On success, returns nil.
func Require(
	ctx context.Context,
	policy PolicyEvaluator,
	userID, tenantID uuid.UUID,
	permissionSlug string,
) error {
	if userID == uuid.Nil {
		return ErrUnauthenticated
	}
	if tenantID == uuid.Nil {
		return ErrTenantScopeRequired
	}
	if policy == nil {
		return fmt.Errorf("rbac: require: %w (no policy wired)", ErrPermissionDenied)
	}
	allowed, err := policy.HasPermission(ctx, userID, tenantID, permissionSlug)
	if err != nil {
		return fmt.Errorf("rbac: require %s: %w", permissionSlug, err)
	}
	if !allowed {
		return ErrPermissionDenied
	}
	return nil
}
