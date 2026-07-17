// Package api: cookies.go centralises session/refresh-token cookie setting so
// every auth endpoint uses identical attributes (HttpOnly, Secure, SameSite,
// Path, Domain) driven from the auth.session.* config (WS-06 DoD).
package api

import (
	"github.com/gofiber/fiber/v2"
)

// CookieConfig carries the attributes every auth cookie shares.
type CookieConfig struct {
	SessionName   string // e.g. lahijan_session
	RefreshName   string // e.g. lahijan_refresh
	Domain        string
	Path          string
	Secure        bool
	SameSite      string // strict | lax | none
	RefreshMaxAge int    // seconds
	SessionMaxAge int    // seconds
}

// setSessionCookie writes the session cookie and the refresh cookie together.
// Call this on register / login / refresh.
func setSessionCookie(c *fiber.Ctx, cfg CookieConfig, sessionValue, refreshValue string) {
	if cfg.SessionName != "" && sessionValue != "" {
		c.Cookie(&fiber.Cookie{
			Name:     cfg.SessionName,
			Value:    sessionValue,
			Domain:   cfg.Domain,
			Path:     cfg.Path,
			Secure:   cfg.Secure,
			HTTPOnly: true,
			SameSite: parseSameSite(cfg.SameSite),
			MaxAge:   cfg.SessionMaxAge,
		})
	}
	if cfg.RefreshName != "" && refreshValue != "" {
		c.Cookie(&fiber.Cookie{
			Name:     cfg.RefreshName,
			Value:    refreshValue,
			Domain:   cfg.Domain,
			Path:     cfg.Path,
			Secure:   cfg.Secure,
			HTTPOnly: true,
			SameSite: parseSameSite(cfg.SameSite),
			MaxAge:   cfg.RefreshMaxAge,
		})
	}
}

// clearSessionCookie expires both cookies so the browser drops them. Used by
// logout.
func clearSessionCookie(c *fiber.Ctx, cfg CookieConfig) {
	for _, name := range []string{cfg.SessionName, cfg.RefreshName} {
		if name == "" {
			continue
		}
		c.Cookie(&fiber.Cookie{
			Name:     name,
			Value:    "",
			Domain:   cfg.Domain,
			Path:     cfg.Path,
			Secure:   cfg.Secure,
			HTTPOnly: true,
			SameSite: parseSameSite(cfg.SameSite),
			MaxAge:   -1,
		})
	}
}

// parseSameSite maps the config string to Fiber's SameSite constant. Unknown
// values default to Lax (the safest broadly-compatible default).
func parseSameSite(s string) string {
	switch s {
	case "strict", "Strict":
		return "strict"
	case "none", "None":
		return "none"
	case "lax", "Lax", "":
		return "lax"
	}
	return "lax"
}
