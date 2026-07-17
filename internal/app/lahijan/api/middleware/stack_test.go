package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestApply_AllSlotsArePassThrough asserts that Apply installs the full stack
// and that every slot calls c.Next(), so a handler at the end of the chain is
// reached. The domain slots are pass-through seams in WS-05.
func TestApply_AllSlotsArePassThrough(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	Apply(app, Options{})

	// A handler at the end of the chain proves every middleware called Next().
	app.Get("/through", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/through", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestApply_ConditionalCORS(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	Apply(app, Options{
		CORS: &CORSConfig{
			AllowOrigins: "https://app.example.com",
			AllowMethods: "GET,POST",
		},
	})
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("OPTIONS", "/x", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	// Fiber's cors middleware handles the preflight and echoes the origin.
	require.Equal(t, "https://app.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestApply_ConditionalLoggerDoesNotBreakChain(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	// The logger slot must call Next() just like the others.
	Apply(app, Options{RequestLogger: true})
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
}

// TestSetTenantID_PropagatesIntoContext confirms that within a single request,
// SetTenantID stores the id both on Locals and into the request context that
// the repository layer reads via database.TenantFromContext.
func TestSetTenantID_PropagatesIntoContext(t *testing.T) {
	t.Parallel()

	tenant := uuid.New()
	var got uuid.UUID

	app := fiber.New()
	app.Get("/t", func(c *fiber.Ctx) error {
		SetTenantID(c, tenant)
		// Read it back the way repositories will: from the request context.
		got, _ = database.TenantFromContext(c.UserContext())
		return c.SendString("ok")
	})

	_, err := app.Test(httptest.NewRequest("GET", "/t", nil), -1)
	require.NoError(t, err)
	require.Equal(t, tenant, got)
}

// TestRequestID_ReturnsEmptyWhenMissing guards the helper against a missing
// requestid slot.
func TestRequestID_ReturnsEmptyWhenMissing(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	var got string
	app.Get("/r", func(c *fiber.Ctx) error {
		got = RequestID(c)
		return c.SendString("ok")
	})
	_, err := app.Test(httptest.NewRequest("GET", "/r", nil), -1)
	require.NoError(t, err)
	require.Empty(t, got)
}
