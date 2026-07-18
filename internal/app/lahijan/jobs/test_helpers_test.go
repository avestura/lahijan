// Package jobs: test_helpers_test.go contains helpers shared by the
// integration test suite. Lives in a _test.go file so the production build
// does not pay for the OTel SDK test exporter.

package jobs

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// installTestTracerProvider installs a fresh SDK-backed TracerProvider with
// the given in-memory span exporter as the global tracer provider. Returns
// the provider (so the caller can Shutdown it) and a restore func that
// puts the previous global back (so other tests are not affected).
//
// Use from one test at a time; the global is process-wide.
func installTestTracerProvider(exp *tracetest.InMemoryExporter) (*sdktrace.TracerProvider, func()) {
	prev := otel.GetTracerProvider()
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("lahijan-test"),
		),
	)
	if err != nil {
		res = resource.Default()
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		// Sampling all so the test sees every span regardless of the
		// (default) ParentBased(AlwaysSample) tracing overhead.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	return provider, func() { otel.SetTracerProvider(prev) }
}
