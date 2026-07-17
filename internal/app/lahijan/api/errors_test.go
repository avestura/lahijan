package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestSendError_ExactEnvelopeShape(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/boom", func(c *fiber.Ctx) error {
		return SendError(c, fiber.StatusBadRequest, CodeBadRequest,
			"email is required", map[string]any{"field": "email"})
	})

	resp := httptest.NewRequest("GET", "/boom", nil)
	body, err := app.Test(resp, -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, body.StatusCode)

	var env map[string]any
	require.NoError(t, json.NewDecoder(body.Body).Decode(&env))

	// The envelope is exactly {error: {code, message, details}}.
	inner, ok := env["error"].(map[string]any)
	require.True(t, ok, "envelope must contain an 'error' object, got %v", env)
	require.Len(t, env, 1, "envelope must have only the 'error' key, got %v", env)
	require.Equal(t, CodeBadRequest, inner["code"])
	require.Equal(t, "email is required", inner["message"])
	require.Equal(t, "email", inner["details"].(map[string]any)["field"])
}

func TestSendError_OmitsDetailsWhenNil(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/boom", func(c *fiber.Ctx) error {
		return SendNotFound(c, "no such thing")
	})

	resp := httptest.NewRequest("GET", "/boom", nil)
	body, err := app.Test(resp, -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, body.StatusCode)

	raw, err := io.ReadAll(body.Body)
	require.NoError(t, err)

	// details is omitempty, so it must not appear in the JSON.
	require.NotContains(t, string(raw), "details")
	require.Contains(t, string(raw), `"code":"not_found"`)
	require.Contains(t, string(raw), `"message":"no such thing"`)
}

func TestConvenienceHelpers_MapToCorrectStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		send     func(c *fiber.Ctx) error
		wantCode string
		wantStat int
	}{
		{"bad_request", func(c *fiber.Ctx) error { return SendBadRequest(c, "x", nil) }, CodeBadRequest, fiber.StatusBadRequest},
		{"unauthorized", func(c *fiber.Ctx) error { return SendUnauthorized(c, "x") }, CodeUnauthorized, fiber.StatusUnauthorized},
		{"forbidden", func(c *fiber.Ctx) error { return SendForbidden(c, "x") }, CodeForbidden, fiber.StatusForbidden},
		{"not_found", func(c *fiber.Ctx) error { return SendNotFound(c, "x") }, CodeNotFound, fiber.StatusNotFound},
		{"internal", func(c *fiber.Ctx) error { return SendInternal(c, "x") }, CodeInternal, fiber.StatusInternalServerError},
		{"not_implemented", func(c *fiber.Ctx) error { return SendNotImplemented(c, "x") }, CodeNotImplemented, fiber.StatusNotImplemented},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := fiber.New()
			app.Get("/x", tc.send)

			body, err := app.Test(httptest.NewRequest("GET", "/x", nil), -1)
			require.NoError(t, err)
			require.Equal(t, tc.wantStat, body.StatusCode)

			var env ErrorEnvelope
			require.NoError(t, json.NewDecoder(body.Body).Decode(&env))
			require.Equal(t, tc.wantCode, env.Error.Code)
			require.Equal(t, "x", env.Error.Message)
		})
	}
}

func TestErrorHandler_MapsFiberErrorToEnvelope(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler()})
	app.Get("/denied", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusForbidden, "membership required")
	})

	body, err := app.Test(httptest.NewRequest("GET", "/denied", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusForbidden, body.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(body.Body).Decode(&env))
	require.Equal(t, CodeForbidden, env.Error.Code)
	require.Equal(t, "membership required", env.Error.Message)
}

func TestErrorHandler_MapsUnknownErrorToInternalEnvelope(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler()})
	app.Get("/boom", func(c *fiber.Ctx) error {
		return errors.New("kaboom")
	})

	body, err := app.Test(httptest.NewRequest("GET", "/boom", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusInternalServerError, body.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(body.Body).Decode(&env))
	require.Equal(t, CodeInternal, env.Error.Code)
}

func TestCodeForStatus_CoversCommonCodes(t *testing.T) {
	t.Parallel()

	cases := map[int]string{
		fiber.StatusBadRequest:            CodeBadRequest,
		fiber.StatusUnauthorized:          CodeUnauthorized,
		fiber.StatusForbidden:             CodeForbidden,
		fiber.StatusNotFound:              CodeNotFound,
		fiber.StatusConflict:              CodeConflict,
		fiber.StatusNotImplemented:        CodeNotImplemented,
		fiber.StatusInternalServerError:   CodeInternal,
		fiber.StatusBadGateway:            CodeInternal, // any 5xx -> internal
		fiber.StatusRequestEntityTooLarge: CodePayloadTooLarge,
	}
	for status, want := range cases {
		got := codeForStatus(status)
		if got != want {
			t.Errorf("codeForStatus(%d) = %q, want %q", status, got, want)
		}
	}
}
