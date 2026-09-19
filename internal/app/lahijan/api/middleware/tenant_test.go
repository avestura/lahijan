// Package middleware: tenant_test.go covers the WS-08 tenant resolver.
// Pure unit tests; no DB required.

package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantWithResolver_HeaderID_SetsContext(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	app := fiber.New()
	app.Use(TenantWithResolver(TenantResolver{}))
	app.Get("/x", func(c *fiber.Ctx) error {
		got, err := database.TenantFromContext(c.UserContext())
		require.NoError(t, err)
		assert.Equal(t, tenant, got)
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set(HeaderTenantID, tenant.String())
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestTenantWithResolver_GarbageID_LeavesUnset(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Use(TenantWithResolver(TenantResolver{}))
	app.Get("/x", func(c *fiber.Ctx) error {
		_, err := database.TenantFromContext(c.UserContext())
		assert.Error(t, err, "garbage tenant id must leave context unset")
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set(HeaderTenantID, "not-a-uuid")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestTenantWithResolver_NoHeader_LeavesUnset(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Use(TenantWithResolver(TenantResolver{}))
	app.Get("/x", func(c *fiber.Ctx) error {
		_, err := database.TenantFromContext(c.UserContext())
		assert.Error(t, err, "missing tenant header must leave context unset")
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode, "middleware must never fail the request")
}

// TestTenantWithResolver_QueryID_SetsContext covers the WS-32 console path:
// the browser's WebSocket API cannot set the X-Tenant-Id header, so the
// dashboard appends ?tenant_id=<uuid> to the upgrade URL. The resolver
// must honour that query form exactly like the header form.
func TestTenantWithResolver_QueryID_SetsContext(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	app := fiber.New()
	app.Use(TenantWithResolver(TenantResolver{}))
	app.Get("/x", func(c *fiber.Ctx) error {
		got, err := database.TenantFromContext(c.UserContext())
		require.NoError(t, err)
		assert.Equal(t, tenant, got, "tenant_id query param must populate context")
		return c.SendString("ok")
	})

	// No headers set — the tenant arrives exclusively via the query string.
	req := httptest.NewRequest("GET", "/x?"+QueryTenantID+"="+tenant.String(), nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

// TestTenantWithResolver_HeaderID_BeatsQueryID pins the precedence rule:
// the header form wins over the query form so a normal API client carrying
// a stray ?tenant_id= is not silently overridden.
func TestTenantWithResolver_HeaderID_BeatsQueryID(t *testing.T) {
	t.Parallel()
	headerTenant := uuid.New()
	queryTenant := uuid.New()
	app := fiber.New()
	app.Use(TenantWithResolver(TenantResolver{}))
	app.Get("/x", func(c *fiber.Ctx) error {
		got, err := database.TenantFromContext(c.UserContext())
		require.NoError(t, err)
		assert.Equal(t, headerTenant, got, "header form must win over query form")
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/x?"+QueryTenantID+"="+queryTenant.String(), nil)
	req.Header.Set(HeaderTenantID, headerTenant.String())
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}
