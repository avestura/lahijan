// Package middleware: rbac_test.go covers the WS-08 RequirePerm middleware.
// Pure unit tests; no DB required.

package middleware

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePolicy is a hand-written PolicyResolver stub.
type fakePolicy struct {
	allowed  bool
	err      error
	calls    int
	lastUID  uuid.UUID
	lastTID  uuid.UUID
	lastSlug string
}

func (f *fakePolicy) HasPermission(_ *fiber.Ctx, uid, tid uuid.UUID, slug string) (bool, error) {
	f.calls++
	f.lastUID = uid
	f.lastTID = tid
	f.lastSlug = slug
	if f.err != nil {
		return false, f.err
	}
	return f.allowed, nil
}

func newPermApp(policy PolicyResolver, setTenant, setUser bool) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if setTenant {
			tenant := uuid.New()
			SetTenantID(c, tenant)
		}
		if setUser {
			SetUserID(c, uuid.New())
		}
		return c.Next()
	})
	return app
}

func TestRequirePerm_Allowed_CallsNext(t *testing.T) {
	t.Parallel()
	policy := &fakePolicy{allowed: true}
	app := newPermApp(policy, true, true)
	app.Get("/p", RequirePerm(policy, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, policy.calls, "policy must be called exactly once")
	assert.Equal(t, "audit.read", policy.lastSlug)
}

func TestRequirePerm_Denied_Returns403(t *testing.T) {
	t.Parallel()
	policy := &fakePolicy{allowed: false}
	app := newPermApp(policy, true, true)
	app.Get("/p", RequirePerm(policy, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
}

func TestRequirePerm_NoUser_Returns401(t *testing.T) {
	t.Parallel()
	policy := &fakePolicy{allowed: true}
	app := newPermApp(policy, true, false)
	app.Get("/p", RequirePerm(policy, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, 0, policy.calls, "policy must not be called when user is missing")
}

func TestRequirePerm_NoTenant_Returns400(t *testing.T) {
	t.Parallel()
	policy := &fakePolicy{allowed: true}
	app := newPermApp(policy, false, true)
	app.Get("/p", RequirePerm(policy, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode, "missing tenant must be 400 tenant_scope_required")
	assert.Equal(t, 0, policy.calls, "policy must not be called when tenant is missing")
}

func TestRequirePerm_NilPolicy_FailsClosed(t *testing.T) {
	t.Parallel()
	app := newPermApp(nil, true, true)
	app.Get("/p", RequirePerm(nil, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode,
		"missing policy is a wiring bug; fail closed with 500")
}

func TestRequirePerm_PolicyError_FailsClosed(t *testing.T) {
	t.Parallel()
	policy := &fakePolicy{err: errors.New("db down")}
	app := newPermApp(policy, true, true)
	app.Get("/p", RequirePerm(policy, "audit.read"), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/p", nil), -1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode,
		"DB errors must surface as 500, not silently deny")
}
