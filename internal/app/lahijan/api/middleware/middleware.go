// Package middleware assembles Lahijan's Fiber middleware stack in the
// canonical order defined in docs/architecture/conventions.md#http-api:
//
//	requestid -> recover -> cors -> logger -> tenant -> auth -> audit -> rbac -> handler
//
// The infrastructure slots (requestid, recover, cors, logger) wrap Fiber's
// stock middlewares. The domain slots (tenant, auth, audit, rbac) are
// pass-through skeletons in WS-05: they define the seam and ordering every
// later security WS (WS-06, WS-08) depends on, without enforcing anything
// yet (there is no auth/rbac system to enforce). Real enforcement lands in
// those WSs by replacing the bodies below.
package middleware

import (
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Locals keys. Fiber stores per-request values under string keys; centralising
// them here keeps handlers and middleware from drifting out of sync.
const (
	// LocalsRequestID is the request id set by the requestid middleware.
	LocalsRequestID = "request_id"
	// LocalsTenantID is the resolved tenant id set by the tenant middleware.
	LocalsTenantID = "tenant_id"
	// LocalsUserID is the authenticated user id set by the auth middleware.
	LocalsUserID = "user_id"
)

// SetTenantID stores the resolved tenant id in the request scope and propagates
// it into the request context for downstream service/repo calls. Domain
// middleware (tenant) and auth use this; repositories read it back via
// database.TenantFromContext.
func SetTenantID(c *fiber.Ctx, tenantID uuid.UUID) {
	c.Locals(LocalsTenantID, tenantID)
	c.SetUserContext(database.WithTenant(c.UserContext(), tenantID))
}

// SetUserID stores the authenticated user id in the request scope.
func SetUserID(c *fiber.Ctx, userID any) {
	c.Locals(LocalsUserID, userID)
}

// RequestID returns the request id stored on the request scope, or "" if none.
func RequestID(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalsRequestID).(string); ok {
		return v
	}
	return ""
}
