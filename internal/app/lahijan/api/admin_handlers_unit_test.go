package api

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/directory"
	"github.com/avestura/lahijan/internal/app/lahijan/settings"
)

// adminUnitApp mounts the generated routes over a Server with no services, so
// only the guard rails that run before any repository call are exercised:
// disabled-feature answers, authentication and body validation. The full
// behaviour is covered by the integration tests.
func adminUnitApp(authed bool) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if authed {
			middleware.SetUserID(c, uuid.New())
		}
		return c.Next()
	})
	apigen.RegisterHandlers(app, &Server{})
	return app
}

func doReq(t *testing.T, app *fiber.App, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestDirectoryEndpointsAnswer501WhenDisabled(t *testing.T) {
	t.Parallel()
	app := adminUnitApp(true)
	id := uuid.NewString()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/directory/connections"},
		{"POST", "/api/v1/admin/directory/connections"},
		{"GET", "/api/v1/admin/directory/connections/" + id},
		{"PATCH", "/api/v1/admin/directory/connections/" + id},
		{"DELETE", "/api/v1/admin/directory/connections/" + id},
		{"POST", "/api/v1/admin/directory/test"},
		{"POST", "/api/v1/admin/directory/connections/" + id + "/sync"},
		{"GET", "/api/v1/admin/directory/connections/" + id + "/groups"},
	} {
		status, _ := doReq(t, app, tc.method, tc.path, "{}")
		assert.Equalf(t, fiber.StatusNotImplemented, status, "%s %s", tc.method, tc.path)
	}
}

func TestAdminUserWritesRequireAuthentication(t *testing.T) {
	t.Parallel()
	app := adminUnitApp(false)
	id := uuid.NewString()
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/admin/users"},
		{"PATCH", "/api/v1/admin/users/" + id},
		{"DELETE", "/api/v1/admin/users/" + id},
		{"PUT", "/api/v1/admin/users/" + id + "/memberships/" + uuid.NewString()},
		{"GET", "/api/v1/permissions"},
	} {
		status, _ := doReq(t, app, tc.method, tc.path, "{}")
		assert.Equalf(t, fiber.StatusUnauthorized, status, "%s %s", tc.method, tc.path)
	}
}

func TestAdminUserWritesRejectBadBodies(t *testing.T) {
	t.Parallel()
	app := adminUnitApp(true)
	id := uuid.NewString()

	status, _ := doReq(t, app, "POST", "/api/v1/admin/users", `{"email":"  "}`)
	assert.Equal(t, fiber.StatusBadRequest, status, "a blank email is rejected before any lookup")
	status, _ = doReq(t, app, "PATCH", "/api/v1/admin/users/"+id, `not json`)
	assert.Equal(t, fiber.StatusBadRequest, status)
	status, _ = doReq(t, app, "PUT", "/api/v1/admin/users/"+id+"/memberships/"+uuid.NewString(), `{"role":""}`)
	assert.Equal(t, fiber.StatusBadRequest, status)

	// An admin cannot delete themselves; the handler decides this before any
	// repository access, so it can be tested here.
	self := uuid.New()
	app2 := fiber.New()
	app2.Use(func(c *fiber.Ctx) error {
		middleware.SetUserID(c, self)
		return c.Next()
	})
	apigen.RegisterHandlers(app2, &Server{})
	status, _ = doReq(t, app2, "DELETE", "/api/v1/admin/users/"+self.String(), "")
	assert.Equal(t, fiber.StatusConflict, status)
}

func TestAdminUserPathHelpers(t *testing.T) {
	t.Parallel()
	assert.True(t, isAdminUserItemPath("/api/v1/admin/users/abc"))
	assert.False(t, isAdminUserItemPath("/api/v1/admin/users/"))
	assert.False(t, isAdminUserItemPath("/api/v1/admin/users"))
	assert.False(t, isAdminUserItemPath("/api/v1/admin/users/abc/topup"), "billing sub-paths keep their own permissions")
	assert.True(t, isAdminUserMembershipPath("/api/v1/admin/users/u/memberships/t"))
	assert.False(t, isAdminUserMembershipPath("/api/v1/admin/users/u/memberships"))
	assert.False(t, isAdminUserMembershipPath("/api/v1/admin/users/u/other/t"))
	assert.False(t, isAdminUserMembershipPath("/api/v1/other"))
}

func TestDirectoryInputAndDTOs(t *testing.T) {
	t.Parallel()

	enabled := false
	pw := "secret"
	in, err := directoryInput(apigen.DirectoryConnectionRequest{
		Kind: "ldap", Name: "corp", Enabled: &enabled, BindPassword: &pw,
		Config: map[string]any{"url": "ldaps://x"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ldap", in.Kind)
	assert.False(t, in.Enabled)
	assert.JSONEq(t, `{"url":"ldaps://x"}`, string(in.Config))
	require.NotNil(t, in.BindPassword)

	in, err = directoryInput(apigen.DirectoryConnectionRequest{Kind: "saml", Name: "sso", Config: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, in.Enabled, "enabled defaults to true")

	now := time.Now()
	dto := toDirectoryConnectionDTO(directory.Connection{
		ID: uuid.New(), Kind: "ldap", Name: "corp", Enabled: true, Active: true, HasSecret: true,
		Config:     []byte(`{"url":"ldaps://x"}`),
		LastSyncAt: &now, LastSyncStatus: "error", LastSyncMessage: "boom", LastSyncUsers: 3, LastSyncGroups: 1,
		ActivationError: "no key",
	})
	assert.Equal(t, "ldaps://x", dto.Config["url"])
	require.NotNil(t, dto.LastSyncMessage)
	assert.Equal(t, "boom", *dto.LastSyncMessage)
	require.NotNil(t, dto.ActivationError)
	assert.EqualValues(t, 3, *dto.LastSyncUsers)
	assert.True(t, dto.Active)

	never := toDirectoryConnectionDTO(directory.Connection{Kind: "saml", Config: []byte(`{}`)})
	assert.Nil(t, never.LastSyncAt)
	assert.Nil(t, never.ActivationError)

	ldapOK := toDirectoryTestDTO(directory.TestResult{OK: true, Code: "ok", Users: 4, Groups: 2})
	require.NotNil(t, ldapOK.Users)
	assert.Equal(t, 4, *ldapOK.Users)
	assert.Nil(t, ldapOK.EntityId)
	samlOK := toDirectoryTestDTO(directory.TestResult{OK: true, Code: "ok", EntityID: "idp", SSOURL: "https://sso"})
	require.NotNil(t, samlOK.EntityId)
	assert.Nil(t, samlOK.Users)
	failed := toDirectoryTestDTO(directory.TestResult{Code: "connect_failed", Detail: "refused"})
	require.NotNil(t, failed.Detail)
	assert.False(t, failed.Ok)
}

func TestMapDirectoryError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want int
	}{
		{directory.ErrNotFound, fiber.StatusNotFound},
		{directory.ErrInvalid, fiber.StatusBadRequest},
		{directory.ErrNameTaken, fiber.StatusConflict},
		{directory.ErrSyncUnsupported, fiber.StatusConflict},
		{directory.ErrCryptoRequired, fiber.StatusInternalServerError},
		{directory.ErrSyncFailed, fiber.StatusBadGateway},
		{directory.ErrLDAPConnect, fiber.StatusBadGateway},
		{errors.New("database is down"), fiber.StatusInternalServerError},
	}
	for _, tc := range cases {
		app := fiber.New()
		app.Get("/", func(c *fiber.Ctx) error { return (&Server{}).mapDirectoryError(c, tc.err) })
		status, _ := doReq(t, app, "GET", "/", "")
		assert.Equalf(t, tc.want, status, "%v", tc.err)
	}
}

func TestSettingsEndpointsAnswer501WhenDisabled(t *testing.T) {
	t.Parallel()
	app := adminUnitApp(true)
	status, _ := doReq(t, app, "GET", "/api/v1/admin/settings", "")
	assert.Equal(t, fiber.StatusNotImplemented, status)
	status, _ = doReq(t, app, "PUT", "/api/v1/admin/settings", `{"registrationEnabled":false}`)
	assert.Equal(t, fiber.StatusNotImplemented, status)
}

func TestToPlatformSettingsDTO(t *testing.T) {
	t.Parallel()
	dto := toPlatformSettingsDTO(settings.State{RegistrationEnabled: false, RegistrationDefault: true, RegistrationOverridden: true})
	assert.False(t, dto.RegistrationEnabled)
	assert.True(t, dto.RegistrationDefault)
	assert.True(t, dto.RegistrationOverridden)
}

func TestRegistrationDisabledErrorsAreForbidden(t *testing.T) {
	t.Parallel()
	for _, err := range []error{session.ErrRegistrationDisabled, idp.ErrSignupDisabled} {
		app := fiber.New()
		app.Get("/", func(c *fiber.Ctx) error {
			if errors.Is(err, session.ErrRegistrationDisabled) {
				return (&Server{}).mapAuthError(c, err)
			}
			return (&Server{}).mapIDPError(c, err)
		})
		status, body := doReq(t, app, "GET", "/", "")
		assert.Equal(t, fiber.StatusForbidden, status)
		assert.Contains(t, body, CodeRegistrationDisabled)
	}
}

// RegisterRoutes ends with a catch-all 404, so anything mounted on the app
// afterwards is unreachable. The River job UI was mounted afterwards and
// answered 404 for exactly this reason; mount such routes BEFORE RegisterRoutes.
func TestRegisterRoutesCatchAllSwallowsLaterRoutes(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Get("/before", func(c *fiber.Ctx) error { return c.SendString("ok") })
	RegisterRoutes(app, &Server{}, nil)
	app.Get("/after", func(c *fiber.Ctx) error { return c.SendString("ok") })

	status, _ := doReq(t, app, "GET", "/before", "")
	assert.Equal(t, fiber.StatusOK, status)
	status, _ = doReq(t, app, "GET", "/after", "")
	assert.Equal(t, fiber.StatusNotFound, status, "a route registered after RegisterRoutes is never reached")
}
