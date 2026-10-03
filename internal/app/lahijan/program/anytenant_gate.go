package program

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
)

// tenantLister returns the tenants a user is a member of.
type tenantLister func(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)

// membershipTenants adapts the memberships repository to a tenantLister.
func membershipTenants(repo *database.MembershipsRepository) tenantLister {
	return func(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
		rows, err := repo.ListForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, m := range rows {
			ids = append(ids, m.TenantID)
		}
		return ids, nil
	}
}

// anyTenantGate guards a page that a browser opens by plain navigation, such as
// River's job UI: such a request cannot carry the X-Tenant-Id header the
// per-route RequirePerm needs. It lets the request through when the signed-in
// user holds the permission in ANY tenant they belong to (platform-wide
// permissions like platform.jobs.read are held through a role in some tenant).
// An unauthenticated request gets 401; everyone else gets 403.
func anyTenantGate(policy middleware.PolicyResolver, tenants tenantLister, permission string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		uid, ok := c.Locals(middleware.LocalsUserID).(uuid.UUID)
		if !ok || uid == uuid.Nil {
			return api.SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		}
		ids, err := tenants(c.UserContext(), uid)
		if err != nil {
			return api.SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
		}
		for _, tenantID := range ids {
			allowed, perr := policy.HasPermission(c, uid, tenantID, permission)
			if perr != nil {
				return api.SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
			}
			if allowed {
				return c.Next()
			}
		}
		return api.SendForbidden(c, permission)
	}
}
