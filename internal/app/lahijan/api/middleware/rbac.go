// Package middleware: rbac.go is the authorization slot.
//
// In WS-05 this is a pass-through seam. WS-08 replaces the body with real
// permission checks: every privileged route calls RequirePerm("scope.action"),
// which looks up the caller's roles for the resolved tenant and rejects with
// api.SendForbidden when the permission is absent. The audit slot (run just
// before this one) records the privileged action.
package middleware

import "github.com/gofiber/fiber/v2"

// RBAC authorises the resolved principal for the current route.
//
// WS-05 behaviour: pass-through. There are no privileged actions defined yet,
// so nothing is rejected. Keeping the slot in the stack fixes the ordering
// contract that WS-08 depends on.
func RBAC() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// TODO(WS-08): RequirePerm("scope.action") per route; reject with
		// api.SendForbidden(c, "permission denied") on denial.
		return c.Next()
	}
}
