// Package middleware: auth.go is the authentication slot.
//
// In WS-05 this is a pass-through seam. WS-06 replaces the body with real
// authentication: parse the Authorization header / session cookie, validate
// the token, and call middleware.SetUserID. Routes that require auth and get
// none are rejected here with the standard error envelope
// (api.SendUnauthorized).
package middleware

import "github.com/gofiber/fiber/v2"

// Auth authenticates the caller and stores the principal on the request scope.
//
// WS-05 behaviour: pass-through. The seeded /api/v1/me endpoint is
// intentionally unauthenticated until WS-06 lands the real session/token
// machinery, so the slot does not reject anonymous requests yet.
func Auth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// TODO(WS-06): parse credential, verify, SetUserID(c, uid); reject with
		// api.SendUnauthorized(c, "authentication required") when missing/invalid
		// on routes that require auth.
		return c.Next()
	}
}
