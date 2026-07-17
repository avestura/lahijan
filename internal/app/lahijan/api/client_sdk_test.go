package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"

	lahijanclient "github.com/avestura/lahijan/pkg/lahijan-client"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

// startTestServer boots the real route registrations onto an ephemeral port
// and returns a client pointed at it, plus a stop func. It exercises the
// generated Go client SDK (pkg/lahijan-client) against the real server, so it
// is the end-to-end proof that the server and client share one contract.
func startTestServer(t *testing.T) (*lahijanclient.ClientWithResponses, func()) {
	t.Helper()

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterRoutes(app, NewServer(ServerDeps{}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = app.Listener(ln) }()

	client, err := lahijanclient.NewClientWithResponses("http://" + ln.Addr().String())
	require.NoError(t, err)

	stop := func() {
		_ = ln.Close()
	}
	return client, stop
}

func TestClientSDK_PingRoundTrip(t *testing.T) {
	t.Parallel()
	client, stop := startTestServer(t)
	defer stop()

	resp, err := client.PingWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200, "pong body must decode")
	require.False(t, resp.JSON200.Pong.IsZero(), "pong timestamp must be set")
}

func TestClientSDK_HealthRoundTrip(t *testing.T) {
	t.Parallel()
	client, stop := startTestServer(t)
	defer stop()

	resp, err := client.GetHealthWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.Equal(t, "ok", string(resp.JSON200.Status))
	require.NotEmpty(t, resp.JSON200.Version)
}

func TestClientSDK_MeReturnsErrorEnvelope(t *testing.T) {
	t.Parallel()
	client, stop := startTestServer(t)
	defer stop()

	// /api/v1/auth/me requires authentication; without credentials the SDK
	// gets the standard 401 envelope (WS-06).
	resp, err := client.GetCurrentUserWithResponse(context.Background())
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode())
	require.NotNil(t, resp.JSON401, "error body must decode into the envelope")

	// The decoded body must match the server-side envelope the api package owns.
	var env ErrorEnvelope
	require.NoError(t, json.Unmarshal(resp.Body, &env))
	require.Equal(t, CodeUnauthorized, env.Error.Code)
}
