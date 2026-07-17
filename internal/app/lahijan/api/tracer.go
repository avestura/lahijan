// Package api: tracer.go provides the api-layer OpenTelemetry tracer handle.
//
// Full OTel wiring (slog handler, OTLP exporter, resource attributes) lands in
// WS-04. Until then, handlers that call Tracer() get the global tracer, which
// is a no-op when no provider is registered. Tests install a real
// in-memory exporter (see server_test.go) to assert spans are emitted.
package api

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name for the api layer.
const tracerName = "lahijan.api"

// Tracer returns the OpenTelemetry tracer for the api layer. It resolves
// against the global tracer provider, so it works whether WS-04 has wired a
// real exporter or not (no-op by default).
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}
