// Package stripe: tracing.go provides the package-local OpenTelemetry
// tracer (per ADR-0016 "Full OTel"). Every public method opens a span
// so a request through the billing module can be cross-referenced with
// Stripe's own logs via the stripe.* attributes.
package stripe

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name for the Stripe driver.
const tracerName = "lahijan.providers.stripe"

// tracer returns the package-local tracer. It resolves against the
// global tracer provider, so it works whether WS-04 has wired a real
// exporter or not (no-op by default).
func tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// startSpan opens a span named "stripe.<name>" and returns the new
// context plus the span. Call `defer span.End()` immediately and
// record any error via setStatus before returning.
func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer().Start(ctx, "stripe."+name, trace.WithAttributes(attrs...))
}

// setStatus records the outcome of a span. A nil error marks it OK; a
// non-nil error marks it ERROR and records the error message.
func setStatus(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	span.SetStatus(codes.Ok, "")
}
