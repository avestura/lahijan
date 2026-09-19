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

// QueryTenantID is the query-string form of the tenant hint. It exists for
// transports that CANNOT set headers — primarily the browser's WebSocket
// API: `new WebSocket(url)` exposes no header setter, so the dashboard's
// interactive console client (WS-32) appends ?tenant_id=<id> to the WS
// upgrade URL and this resolver picks it up exactly like the header form.
//
// This is NOT a privilege grant: the tenant value is only a hint. Actual
// membership is enforced downstream by RequirePerm (per-route), which
// verifies the authenticated user belongs to the resolved tenant. A spoofed
// query hint therefore cannot grant access to a tenant the caller does not
// belong to — the same property the X-Tenant-Id header already has.
const QueryTenantID = "tenant_id"

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
// Resolution order:
//  1. X-Tenant-Id (UUID header) — the fast path for normal API requests.
//  2. tenant_id (UUID query param) — fallback for transports that cannot
//     set headers (the WS-32 console WebSocket; see QueryTenantID).
//  3. X-Tenant-Slug (header) — DB lookup, for clients that only know the
//     URL slug.
//
// The header form wins over the query form so a normal API client that
// happens to carry a stray ?tenant_id= is not silently overridden.
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

		// 2) tenant_id query param — WebSocket fallback. The browser's
		// WebSocket API cannot set custom headers, so the dashboard's
		// interactive console client passes the tenant via ?tenant_id=.
		// Membership is still enforced downstream by RequirePerm.
		if raw := c.Query(QueryTenantID); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				SetTenantID(c, id)
				return c.Next()
			}
			// fall through on parse error.
		}

		// 3) X-Tenant-Slug — DB lookup, only when a TenantsRepository is wired.
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
