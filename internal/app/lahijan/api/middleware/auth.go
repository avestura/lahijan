// Package middleware: auth.go is the authentication slot.
//
// WS-06 wires the real resolver: it reads the session cookie OR the
// Authorization: Bearer PAT, validates the credential against the database,
// resolves a user id, and stores it on the request scope via SetUserID.
// Requests that carry no credential (or an invalid one) pass through
// anonymously; per-route enforcement is the handler's job (api.requireUser
// rejects with 401 when an authenticated principal is required).
//
// The resolver also derives the request locale from the Accept-Language header
// so every downstream i18n.T call renders in the right language.
package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// CookieConfig mirrors api.CookieConfig's cookie-name subset but lives here so
// the middleware does not import the api package (which would create a cycle:
// api imports middleware for LocalsUserID). program.buildAuthDeps keeps them in
// sync.
type CookieConfig struct {
	SessionName string
	RefreshName string
}

// AuthResolver carries the dependencies the auth slot needs. Build it once at
// bootstrap from conf + repos + services and pass to AuthWithResolver.
type AuthResolver struct {
	Cookies      CookieConfig
	Signer       *secrets.Signer
	SessionsRepo *database.SessionsRepository
	PATService   *pat.Service
}

// AuthWithResolver returns a Fiber handler that resolves the caller's identity
// via the session cookie or a Bearer PAT. A missing or invalid credential
// leaves the request anonymous (no LocalsUserID); privileged routes reject
// anonymous requests via api.requireUser.
//
// On every authenticated request the slot also:
//   - touches sessions.last_seen_at (best-effort), and
//   - sets the request locale from Accept-Language.
func AuthWithResolver(r AuthResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.SetUserContext(withLocaleFromHeader(c.UserContext(), c.Get("Accept-Language")))

		// Session cookie first (browser flow).
		if r.Cookies.SessionName != "" && r.Signer != nil && r.SessionsRepo != nil {
			if raw := c.Cookies(r.Cookies.SessionName); raw != "" {
				if uid, ok := resolveSession(c, r, raw); ok {
					SetUserID(c, uid)
					return c.Next()
				}
			}
		}

		// Authorization: Bearer <pat> (programmatic flow).
		if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if token != "" && r.PATService != nil {
				if principal, err := r.PATService.Authenticate(c.UserContext(), token); err == nil {
					SetUserID(c, principal.UserID)
					return c.Next()
				}
			}
		}

		return c.Next()
	}
}

// resolveSession validates the raw session cookie value, looks up the session by
// its hash, and returns the owning user id when the session is live. Touches
// last_seen_at best-effort; a touch failure never blocks the request.
func resolveSession(c *fiber.Ctx, r AuthResolver, raw string) (uuid.UUID, bool) {
	hash, err := r.Signer.Verify(raw)
	if err != nil {
		return uuid.Nil, false
	}
	sess, err := r.SessionsRepo.GetByTokenHash(c.UserContext(), hash)
	if err != nil {
		return uuid.Nil, false
	}
	if sess.RevokedAt != nil || time.Now().After(sess.ExpiresAt) {
		return uuid.Nil, false
	}
	go touchSession(r, sess.ID)
	return sess.UserID, true
}

// touchSession updates sessions.last_seen_at. Runs in its own goroutine so it
// never blocks the request; errors are silently dropped (best-effort).
func touchSession(r AuthResolver, sessionID uuid.UUID) {
	cctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.SessionsRepo.Touch(cctx, sessionID)
}

// withLocaleFromHeader derives the locale from Accept-Language and stashes it on
// the context for downstream i18n.T calls.
func withLocaleFromHeader(ctx context.Context, header string) context.Context {
	return i18n.WithLocale(ctx, i18n.ResolveAcceptLanguage(header))
}

// Auth is the WS-05 pass-through slot kept for compatibility with stacks that
// do not yet wire a resolver (tests). program.Start always calls
// AuthWithResolver instead.
func Auth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.SetUserContext(withLocaleFromHeader(c.UserContext(), c.Get("Accept-Language")))
		return c.Next()
	}
}
