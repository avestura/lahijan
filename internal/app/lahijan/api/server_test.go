package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecordingTracerProvider builds a TracerProvider that exports every ended
// span into exporter (an in-memory recorder). Used to assert the api layer
// emits spans without needing the full WS-04 OTLP wiring.
func newRecordingTracerProvider(exporter *tracetest.InMemoryExporter) *trace.TracerProvider {
	return trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithSpanProcessor(trace.NewBatchSpanProcessor(exporter)),
	)
}

// newTestApp wires the real route registrations onto a fresh Fiber app so the
// tests exercise the generated routing table end to end.
func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New()
	RegisterRoutes(app)
	return app
}

func TestPing_ReturnsPongWithTimestamp(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/ping", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var pong apigen.Pong
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pong))
	require.False(t, pong.Pong.IsZero(), "pong timestamp must be set")
}

func TestPing_EmitsTraceSpan(t *testing.T) {
	t.Parallel()

	// Build a dedicated tracer provider whose spans land in an in-memory
	// recorder, and inject its tracer into the server. This is fully isolated
	// from the process-global tracer provider, so parallel ping tests do not
	// pollute this assertion.
	exporter := tracetest.NewInMemoryExporter()
	tp := newRecordingTracerProvider(exporter)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	app := fiber.New()
	apigen.RegisterHandlers(app, NewServerWithTracer(tp.Tracer("lahijan.api")))

	_, err := app.Test(httptest.NewRequest("GET", "/api/v1/ping", nil), -1)
	require.NoError(t, err)

	// Force the batch span processor to flush the ended span to the recorder.
	require.NoError(t, tp.ForceFlush(context.Background()))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "ping handler must emit exactly one span")
	require.Equal(t, "ping", spans[0].Name)
}

func TestPing_BodyShapeIsExact(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/ping", nil), -1)
	require.NoError(t, err)

	raw := make(map[string]any)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	require.Len(t, raw, 1, "pong body must contain only the 'pong' key, got %v", raw)
	_, ok := raw["pong"].(string)
	require.True(t, ok, "'pong' must be a string timestamp")
}

func TestHealth_ReturnsOkAndVersion(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/health", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var h apigen.Health
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&h))
	require.Equal(t, apigen.HealthStatusOk, h.Status)
	require.NotEmpty(t, h.Version, "health must report the build version")
}

func TestMe_ReturnsEnvelopeError(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/me", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotImplemented, resp.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	require.Equal(t, CodeNotImplemented, env.Error.Code)
}

func TestUnknownRoute_ReturnsEnvelopeNotFound(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)

	// A request to an undefined path must come back as the standard error
	// envelope (not Fiber's default "Cannot GET /x" body).
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/does-not-exist", nil), -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	require.Equal(t, CodeNotFound, env.Error.Code)
}
