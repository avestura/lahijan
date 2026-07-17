// Package middleware: tenant.go resolves the request's tenant scope.
//
// The tenant id is taken from the `X-Tenant-Id` request header (a UUID). When
// the header is absent or unparseable, the request proceeds with no tenant in
// the context; privileged tenant-scoped routes then fail closed via
// RequirePerm. The middleware does NOT verify the caller's membership in the
// tenant — that check is deferred to RequirePerm (per-route) so the global
// middleware order in conventions.md (tenant -> auth -> audit -> rbac) can
// resolve the tenant hint before auth resolves the user.
//
// WS-08 also adds the slug form `X-Tenant-Slug` for clients that only know
// the tenant's URL slug (e.g. the marketing site). The slug is looked up via
// TenantsRepository; an unknown slug leaves the tenant unset.
package middleware

import (
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// HeaderTenantID is the canonical request header carrying the resolved tenant
// UUID. Browser and CLI clients set this once they pick a tenant (the
// dashboard's tenant switcher updates the header on click).
const HeaderTenantID = "X-Tenant-Id"

// HeaderTenantSlug is the fallback header carrying a tenant slug, for clients
// that only know the URL-friendly identifier (e.g. a deep link from the
// marketing site). The middleware resolves the slug to an id via the
// TenantsRepository.
const HeaderTenantSlug = "X-Tenant-Slug"

// TenantResolver carries the deps the tenant slot needs when at least one of
// the headers is supplied. Build it once at bootstrap and pass to
// TenantWithResolver; the legacy Tenant() pass-through is kept for tests.
type TenantResolver struct {
	Tenants *database.TenantsRepository
}

// TenantWithResolver returns a Fiber handler that resolves the request's
// tenant scope. The handler never fails the request: a missing/unparseable/
// unknown tenant leaves the scope unset and privileged routes reject later.
//
// Resolution order: X-Tenant-Id wins (cheap, no DB hit); X-Tenant-Slug is the
// fallback (one DB lookup by slug).
func TenantWithResolver(r TenantResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 1) X-Tenant-Id (UUID) — the fast path.
		if raw := c.Get(HeaderTenantID); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				SetTenantID(c, id)
				return c.Next()
			}
			// fall through on parse error: maybe the caller meant the slug
			// and put garbage in the id header.
		}

		// 2) X-Tenant-Slug — DB lookup, only when a TenantsRepository is wired.
		if r.Tenants != nil {
			if slug := c.Get(HeaderTenantSlug); slug != "" {
				tenant, err := r.Tenants.GetBySlug(c.UserContext(), slug)
				if err == nil {
					SetTenantID(c, tenant.ID)
				}
				// Unknown slug: leave tenant unset; privileged routes fail closed.
			}
		}
		return c.Next()
	}
}

// Tenant is the WS-05 pass-through slot kept for compatibility with stacks
// that do not wire a resolver (tests, dev runs without a DB). program.Start
// always calls TenantWithResolver instead.
func Tenant() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}
