// Package middleware: audit.go is the audit-log slot.
//
// Per docs/architecture/conventions.md#security, every state-changing
// privileged action emits an audit event BEFORE the side effect and updates it
// with the result. In WS-05 this slot is a pass-through seam: WS-08 wires the
// real append-only audit log (internal/app/lahijan/database.AuditLogRepository).
//
// The slot sits between auth and rbac so it can observe the resolved principal
// (user id from auth) and the about-to-be-checked permission, and so it always
// runs before the handler that performs the privileged action.
package middleware

import "github.com/gofiber/fiber/v2"

// Audit records privileged actions for the append-only audit log.
//
// WS-05 behaviour: pass-through. The audit subsystem lands in WS-08.
func Audit() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// TODO(WS-08): for privileged routes, open an audit row before the
		// handler runs (actor, action, target, request_id), then patch it with
		// the result afterwards. Use the database.AuditLogRepository.
		return c.Next()
	}
}
