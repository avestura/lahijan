// Package middleware: tenant.go is the tenant-resolution slot.
//
// In WS-05 this is a pass-through seam: it runs in the right place in the
// stack but does not yet resolve a tenant (there is no auth, so no tenant can
// be derived). WS-06 (auth) and WS-08 (rbac) will fill in the real resolution:
// read the tenant hint (header/subdomain/jwt claim), validate the caller's
// membership, and call middleware.SetTenantID so repositories scope correctly.
package middleware

import "github.com/gofiber/fiber/v2"

// Tenant resolves the request's tenant and stores it on the request scope.
//
// WS-05 behaviour: pass-through. Unauthenticated routes (/health, /ping) must
// work without a tenant, so the slot never fails the request on its own. Real
// fail-closed enforcement happens at the rbac slot for privileged routes.
func Tenant() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// TODO(WS-06/WS-08): resolve tenant id from the authenticated principal
		// (e.g. JWT claim / membership lookup) and call SetTenantID(c, id).
		return c.Next()
	}
}
