// Package seaweedfs: tracing.go provides the package-local OpenTelemetry tracer
// (per ADR-0016 "Full OTel"). Every public method opens a span so a request
// through the storage module can be cross-referenced with the SeaweedFS
// daemon's own access logs via the seaweedfs.bucket attribute.
package seaweedfs

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name for the SeaweedFS driver.
const tracerName = "lahijan.providers.seaweedfs"

// tracer returns the package-local tracer. It resolves against the global
// tracer provider, so it works whether WS-04 has wired a real exporter or
// not (no-op by default).
func tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// startSpan opens a span named "seaweedfs.<name>" and returns the new context
// plus the span. Call `defer span.End()` immediately and record any error
// via setStatus before returning.
func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer().Start(ctx, "seaweedfs."+name, trace.WithAttributes(attrs...))
}

// setStatus records the outcome of a span. A nil error marks it OK; a non-nil
// error marks it ERROR and records the error message.
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

// bucketAttr returns the attribute that records the canonical bucket name on
// a span. Empty name returns a no-op (no attribute added).
func bucketAttr(bucket string) attribute.KeyValue {
	return attribute.String("seaweedfs.bucket", bucket)
}

// accessKeyAttr returns the attribute that records the access key an IAM
// operation is about. Used to cross-reference a slow PutIdentity with the
// Filer's access logs.
func accessKeyAttr(accessKey string) attribute.KeyValue {
	return attribute.String("seaweedfs.access_key", accessKey)
}
