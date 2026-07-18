// Package hostfuncs: tracing.go wraps OpenTelemetry span creation for
// every host call. Per the WS-10b DoD "every host function call is
// observable in OTel traces", each host function opens a span named
// wasm.host.<module>.<fn> carrying the plugin id, the permission slug,
// and the result code.
//
// Until WS-04 ships a real OTLP exporter, the global tracer/meter
// providers are no-ops; this code still emits, it just goes nowhere
// until a provider is installed.
package hostfuncs

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name for the host-function layer.
const tracerName = "lahijan.wasm.hostfuncs"

// tracer returns the OTel tracer for the host-function layer, resolved
// against the global provider. No-op when no provider is registered.
func tracer() trace.Tracer { return otel.Tracer(tracerName) }

// startSpan opens a span for a host call. The returned context carries
// the span so further downstream calls (DB, HTTP) inherit it. Callers
// must defer-span.End and call recordResult before returning.
func startSpan(ctx context.Context, module, fn string, pluginID uuid.UUID, slug string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attribute.String("wasm.module", module),
		attribute.String("wasm.function", fn),
		attribute.String("plugin.id", pluginID.String()),
		attribute.String("permission.slug", slug),
	}
	return tracer().Start(
		ctx, "wasm.host."+module+"."+fn,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
}

// recordResult writes the host function's status code onto the span.
// Negative codes mark the span as ERROR; positive and zero mark it OK.
// The Explain() label is attached so dashboards can group by code.
func recordResult(span trace.Span, code int32) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.Int64("wasm.host.result_code", int64(code)),
		attribute.String("wasm.host.result", Explain(code)),
	)
	if code < 0 {
		span.SetStatus(codes.Error, Explain(code))
	}
}
