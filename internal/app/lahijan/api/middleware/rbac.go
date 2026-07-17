// Package middleware: rbac.go holds the global RBAC slot in the canonical
// middleware order, plus the per-route RequirePerm helper every privileged
// handler chains.
//
// The global RBAC slot is intentionally a no-op pass-through: per-route
// permission checks happen via RequirePerm because each route needs its own
// permission slug. Keeping the global slot in the order preserves the
// conventions.md contract and gives future WSs a single place to insert
// cross-cutting checks (e.g. IP allowlists, rate limiting by role).
//
// RequirePerm does three things, in order, and fails closed at every step:
//
//  1. Resolves the user id (set by the Auth middleware). Missing -> 401.
//  2. Resolves the tenant id (set by the Tenant middleware). Missing -> 400
//     "tenant scope required" (the client forgot to send X-Tenant-Id).
//  3. Calls the rbac.PolicyEvaluator; denied -> 403 forbidden with the
//     standard error envelope.
//
// RequirePerm is a per-route middleware. Mount it on the routes that need it:
//
//	app.Get("/api/v1/audit",
//	    middleware.RequirePerm(deps.Policy, rbac.PermAuditRead),
//	    server.ListAudit)
package middleware

import (
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RBAC authorises the resolved principal for the current route.
//
// WS-08 behaviour: pass-through. Per-route enforcement happens via
// RequirePerm (below), which needs the specific permission slug for the route.
// Keeping this slot in the stack preserves the middleware-order contract.
func RBAC() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// PolicyResolver is the seam RequirePerm talks to. The api package wires a
// *rbac.Evaluator here; tests can pass a fake.
type PolicyResolver interface {
	HasPermission(
		ctx fiber.Ctx,
		userID, tenantID uuid.UUID,
		permissionSlug string,
	) (bool, error)
}

// adapterCtx shims the rbac.PolicyEvaluator signature (which takes
// context.Context) into the PolicyResolver signature (which takes fiber.Ctx)
// so the api package can wire a *rbac.Evaluator without the rbac package
// importing Fiber.
type adapterCtx struct{ inner rbac.PolicyEvaluator }

// HasPermission implements PolicyResolver by delegating to the wrapped
// rbac.PolicyEvaluator with the request's user context.
func (a *adapterCtx) HasPermission(c fiber.Ctx, userID, tenantID uuid.UUID, slug string) (bool, error) {
	return a.inner.HasPermission(c.UserContext(), userID, tenantID, slug)
}

// NewPolicyResolver wraps a rbac.PolicyEvaluator as a PolicyResolver. Pass
// the result to RequirePerm.
func NewPolicyResolver(inner rbac.PolicyEvaluator) PolicyResolver {
	if inner == nil {
		return nil
	}
	return &adapterCtx{inner: inner}
}

// RequirePerm returns a per-route Fiber middleware that rejects the request
// unless the caller holds the given permission in the resolved tenant. The
// slug should be one of the rbac.Perm* constants; passing an unknown slug is
// always denied (PermissionExists returns false).
//
// Mount this on every privileged route. The order in conventions.md
// (requestid -> recover -> cors -> logger -> tenant -> auth -> audit -> rbac
// -> handler) is preserved; this slot sits between rbac and handler.
func RequirePerm(policy PolicyResolver, permissionSlug string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 1) User must be resolved (Auth middleware).
		uid, ok := userIDFromLocals(c)
		if !ok {
			return sendUnauthenticated(c)
		}
		// 2) Tenant scope must be set (Tenant middleware).
		tenantID, ok := tenantIDFromLocals(c)
		if !ok {
			// The client forgot to send X-Tenant-Id (or sent garbage).
			// 400 is the right code: the request is malformed for this route.
			return sendTenantScopeRequired(c)
		}
		// 3) Policy check.
		if policy == nil {
			// Bootstrap wiring bug: program.Start should always wire a real
			// evaluator. Fail closed rather than silently allowing.
			return sendInternal(c)
		}
		allowed, err := policy.HasPermission(*c, uid, tenantID, permissionSlug)
		if err != nil {
			// DB error or evaluator fault: fail closed and surface 500.
			return sendInternal(c)
		}
		if !allowed {
			return sendForbidden(c, permissionSlug)
		}
		return c.Next()
	}
}

// userIDFromLocals reads the user id set by the Auth middleware. Accepts
// either uuid.UUID or *uuid.UUID shapes (the auth resolver stores uuid.UUID).
func userIDFromLocals(c *fiber.Ctx) (uuid.UUID, bool) {
	v := c.Locals(LocalsUserID)
	if v == nil {
		return uuid.Nil, false
	}
	switch t := v.(type) {
	case uuid.UUID:
		return t, t != uuid.Nil
	case *uuid.UUID:
		if t == nil {
			return uuid.Nil, false
		}
		return *t, *t != uuid.Nil
	}
	return uuid.Nil, false
}

// tenantIDFromLocals reads the tenant id set by the Tenant middleware.
func tenantIDFromLocals(c *fiber.Ctx) (uuid.UUID, bool) {
	v := c.Locals(LocalsTenantID)
	if v == nil {
		return uuid.Nil, false
	}
	t, ok := v.(uuid.UUID)
	if !ok || t == uuid.Nil {
		return uuid.Nil, false
	}
	return t, true
}

// sendUnauthenticated writes the standard 401 envelope. The i18n key matches
// the one WS-06 uses for the same condition so the client sees a consistent
// message regardless of which middleware rejected.
func sendUnauthenticated(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(envelope(
		"unauthorized",
		i18n.T(c.UserContext(), "auth.err_unauthorized", nil),
	))
}

// sendTenantScopeRequired writes the standard 400 envelope. The dedicated
// code "tenant_scope_required" lets clients distinguish "you forgot the
// X-Tenant-Id header" from a generic malformed-body 400.
func sendTenantScopeRequired(c *fiber.Ctx) error {
	return c.Status(fiber.StatusBadRequest).JSON(envelope(
		"tenant_scope_required",
		i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil),
	))
}

// sendForbidden writes the standard 403 envelope. The detail includes the
// missing permission slug so operator-side debugging is easier; end users see
// the localised "permission denied" message.
func sendForbidden(c *fiber.Ctx, slug string) error {
	return c.Status(fiber.StatusForbidden).JSON(envelopeDetails(
		"forbidden",
		i18n.T(c.UserContext(), "auth.err_forbidden", nil),
		map[string]any{"permission": slug},
	))
}

// sendInternal writes the standard 500 envelope (no detail leak).
func sendInternal(c *fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(envelope(
		"internal",
		i18n.T(c.UserContext(), "auth.err_internal", nil),
	))
}

// envelope is the local shape matching api.ErrorEnvelope so this file does
// not need to import the api package (which would create a cycle: api imports
// middleware for LocalsUserID).
type envBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type env struct {
	Error envBody `json:"error"`
}

func envelope(code, message string) env {
	return env{Error: envBody{Code: code, Message: message}}
}

func envelopeDetails(code, message string, details map[string]any) env {
	return env{Error: envBody{Code: code, Message: message, Details: details}}
}
