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
