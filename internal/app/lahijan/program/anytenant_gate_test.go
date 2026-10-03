package program

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
)

// grantPolicy allows a permission only in the listed tenants.
type grantPolicy struct {
	allowIn map[uuid.UUID]bool
	err     error
}

func (g grantPolicy) HasPermission(_ *fiber.Ctx, _, tenantID uuid.UUID, _ string) (bool, error) {
	return g.allowIn[tenantID], g.err
}

func gateStatus(t *testing.T, user *uuid.UUID, policy grantPolicy, tenants tenantLister) int {
	t.Helper()
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if user != nil {
			middleware.SetUserID(c, *user)
		}
		return c.Next()
	})
	app.Use("/ui", anyTenantGate(policy, tenants, "platform.jobs.read"), func(c *fiber.Ctx) error {
		return c.SendString("page")
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/ui", nil), -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestAnyTenantGate(t *testing.T) {
	t.Parallel()
	user := uuid.New()
	home, admin := uuid.New(), uuid.New()
	list := func(ids ...uuid.UUID) tenantLister {
		return func(context.Context, uuid.UUID) ([]uuid.UUID, error) { return ids, nil }
	}

	// Held in only one of the user's tenants: allowed, no tenant header needed.
	assert.Equal(t, 200, gateStatus(t, &user, grantPolicy{allowIn: map[uuid.UUID]bool{admin: true}}, list(home, admin)))
	// Held nowhere: forbidden.
	assert.Equal(t, 403, gateStatus(t, &user, grantPolicy{}, list(home, admin)))
	// A user with no memberships: forbidden.
	assert.Equal(t, 403, gateStatus(t, &user, grantPolicy{}, list()))
	// Not signed in: unauthorized.
	assert.Equal(t, 401, gateStatus(t, nil, grantPolicy{}, list(home)))
	// Lookup or policy failures fail closed.
	boom := func(context.Context, uuid.UUID) ([]uuid.UUID, error) { return nil, errors.New("db") }
	assert.Equal(t, 500, gateStatus(t, &user, grantPolicy{}, boom))
	assert.Equal(t, 500, gateStatus(t, &user, grantPolicy{err: errors.New("policy")}, list(home)))
}
