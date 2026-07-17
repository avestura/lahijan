// Package rbac: policy.go is the policy evaluator that RequirePerm calls.
//
// It answers one question: "does user U have permission P in tenant T?" The
// answer is computed by joining memberships + role_permissions in Postgres.
// Callers MUST pass the tenant id from the request context (middleware
// resolves it once); the evaluator does not fall back to the context's tenant
// so the check is explicit at every privileged site.
//
// Caching: MVP does no caching. A short-TTL in-process cache (or River-backed
// invalidation) is a Phase 7 candidate; the table is small and the join is
// indexed, so per-request evaluation is fast enough until profiled otherwise.
package rbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// PolicyEvaluator answers "does user U have permission P in tenant T?".
//
// RequirePerm calls this once per privileged request. Implementations MUST be
// safe for concurrent use. A failure to reach the DB (or any unexpected error)
// returns (false, error); callers MUST fail closed by treating the error as
// "deny" and surfacing a 500 envelope to the client.
type PolicyEvaluator interface {
	// HasPermission returns true when the user holds the permission within
	// the tenant (directly via their role's grant bundle, or via the
	// platform.admin override).
	HasPermission(
		ctx context.Context,
		userID, tenantID uuid.UUID,
		permissionSlug string,
	) (bool, error)

	// PermissionsForUser returns the set of permission slugs the user holds
	// within the tenant. Used by the audit log API to render what the actor
	// could do, and by tests to assert role grants.
	PermissionsForUser(
		ctx context.Context,
		userID, tenantID uuid.UUID,
	) ([]string, error)
}

// PermissionChecker is the function signature RequirePerm uses; it's the same
// shape as PolicyEvaluator.HasPermission but exported as a func type so the
// middleware can swap in a closure that closes over the resolved IDs.
type PermissionChecker func(
	ctx context.Context,
	userID, tenantID uuid.UUID,
	permissionSlug string,
) (bool, error)

// membershipLookup is the persistence seam the evaluator talks to. It's a
// subset of *database.MembershipsRepository + *database.RBACRepository
// narrowed to exactly the queries the evaluator needs, so tests can fake it
// without spinning up Postgres.
type membershipLookup interface {
	// RoleSlugForUser returns the role slug for the user's membership in the
	// tenant, or ("", false, nil) if the user is not a member.
	RoleSlugForUser(ctx context.Context, userID, tenantID uuid.UUID) (string, bool, error)
	// PermissionSlugsForUser returns every permission slug granted to the
	// user via their membership's role in the tenant.
	PermissionSlugsForUser(ctx context.Context, userID, tenantID uuid.UUID) ([]string, error)
}

// Evaluator is the DB-backed PolicyEvaluator. Construct one at bootstrap and
// share it across requests.
type Evaluator struct {
	lookup membershipLookup
}

// NewEvaluator builds an Evaluator backed by the given lookup. The lookup is
// typically a *database.Repos, but tests can pass any implementation of the
// unexported membershipLookup interface.
func NewEvaluator(lookup membershipLookup) *Evaluator {
	return &Evaluator{lookup: lookup}
}

// HasPermission implements PolicyEvaluator.
//
// Short-circuits on the platform.admin role so superusers bypass every check
// without needing the full permission catalog synced to their role row. This
// is the ONLY bypass; every other role falls through to the catalog.
func (e *Evaluator) HasPermission(
	ctx context.Context,
	userID, tenantID uuid.UUID,
	permissionSlug string,
) (bool, error) {
	roleSlug, ok, err := e.lookup.RoleSlugForUser(ctx, userID, tenantID)
	if err != nil {
		return false, fmt.Errorf("rbac: lookup role for %s in %s: %w", userID, tenantID, err)
	}
	if !ok {
		// No membership in this tenant at all -> deny.
		return false, nil
	}
	if roleSlug == RolePlatformAdmin {
		// platform.admin bypasses every permission check. The slug is still
		// asserted against the registry so callers cannot request undefined
		// permissions by accident.
		return PermissionExists(permissionSlug), nil
	}
	slugs, err := e.lookup.PermissionSlugsForUser(ctx, userID, tenantID)
	if err != nil {
		return false, fmt.Errorf("rbac: lookup permissions for %s in %s: %w", userID, tenantID, err)
	}
	for _, s := range slugs {
		if s == permissionSlug {
			return true, nil
		}
	}
	return false, nil
}

// PermissionsForUser implements PolicyEvaluator.
func (e *Evaluator) PermissionsForUser(
	ctx context.Context,
	userID, tenantID uuid.UUID,
) ([]string, error) {
	roleSlug, ok, err := e.lookup.RoleSlugForUser(ctx, userID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("rbac: lookup role for %s in %s: %w", userID, tenantID, err)
	}
	if !ok {
		return nil, nil
	}
	if roleSlug == RolePlatformAdmin {
		// Superuser: returns the full registry so the API can render the
		// catalog for the caller.
		return allPermissionSlugs(), nil
	}
	return e.lookup.PermissionSlugsForUser(ctx, userID, tenantID)
}
