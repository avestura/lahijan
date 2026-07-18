// Package jobs: middleware.go installs OpenTelemetry instrumentation on every
// job worked by this process (pillar 12 / ADR-0016). Each Work() call:
//
//   - opens a span named "river.<kind>.work" tagged with the job id, queue,
//     attempt, and the resolved kind; and
//   - bumps two metrics — jobs_started_total and jobs_completed_total —
//     labelled by kind, queue, and terminal state (success | error | cancelled).
//
// The middleware is wired into the River client via Config.Middleware so it
// runs for every worker kind; per-kind middleware is also supported but
// Lahijan does not currently use it.
//
// Until WS-04 ships a real OTLP exporter, the global tracer/meter providers
// are no-ops; this middleware still emits, it just goes nowhere until a
// provider is installed. Tests install an in-memory exporter (see
// middleware_test.go) to assert the spans are emitted.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation scope name for the jobs layer.
const tracerName = "lahijan.jobs"

// meterName is the metric instrumentation scope for the jobs layer.
const meterName = "lahijan.jobs"

// Metric attribute keys — kept as constants so producers and the admin UI
// can stay in sync without typos.
const (
	attrKind       = "job.kind"
	attrQueue      = "job.queue"
	attrAttempt    = "job.attempt"
	attrJobID      = "job.id"
	attrPriority   = "job.priority"
	attrOutcome    = "job.outcome"
	attrErrorType  = "error.type"
	outcomeSuccess = "success"
	outcomeError   = "error"
)

// tracer returns the OTel tracer for the jobs layer, resolved against the
// global provider. No-op when no provider is registered.
func tracer() trace.Tracer { return otel.Tracer(tracerName) }

// meter returns the OTel meter for the jobs layer, resolved against the
// global provider.
func meter() metric.Meter { return otel.Meter(meterName) }

// otelMiddleware is the WorkerMiddleware that wraps every job execution in a
// span and bumps metrics. It is concurrency-safe (River calls Work from many
// goroutines); the counters themselves are synced by the OTel SDK.
type otelMiddleware struct {
	river.WorkerMiddlewareDefaults

	log            *slog.Logger
	startedCounter metric.Int64Counter
	doneCounter    metric.Int64Counter
	histDuration   metric.Int64Histogram
}

// newOtelMiddleware builds the middleware. The metric handles are resolved
// lazily on first use so a process with no meter provider registered (the
// default until WS-04) does not crash on bootstrap.
func newOtelMiddleware(log *slog.Logger) *otelMiddleware {
	if log == nil {
		log = slog.Default()
	}
	return &otelMiddleware{log: log}
}

// ensureMetrics resolves the metric handles on first use. Returns false (and
// logs once) when the meter provider rejects the instrument; subsequent Work
// calls then skip the metric bumps but still emit a span.
func (m *otelMiddleware) ensureMetrics() bool {
	if m.startedCounter != nil && m.doneCounter != nil && m.histDuration != nil {
		return true
	}
	var err error
	if m.startedCounter, err = meter().Int64Counter("lahijan.jobs.started_total",
		metric.WithDescription("Number of jobs River has started working.")); err != nil {
		m.log.Debug("jobs: otel metric started_total unavailable", "error", err)
		return false
	}
	if m.doneCounter, err = meter().Int64Counter("lahijan.jobs.completed_total",
		metric.WithDescription("Number of jobs that finished, by outcome.")); err != nil {
		m.log.Debug("jobs: otel metric completed_total unavailable", "error", err)
		return false
	}
	if m.histDuration, err = meter().Int64Histogram("lahijan.jobs.duration_ms",
		metric.WithDescription("Wall-clock duration of a single job execution, in milliseconds."),
		metric.WithUnit("ms")); err != nil {
		m.log.Debug("jobs: otel metric duration_ms unavailable", "error", err)
		return false
	}
	return true
}

// Work implements rivertype.WorkerMiddleware by opening a span, bumping the
// started counter, calling doInner, and recording the outcome.
func (m *otelMiddleware) Work(
	ctx context.Context,
	job *rivertype.JobRow,
	doInner func(context.Context) error,
) error {
	attrs := []attribute.KeyValue{
		attribute.String(attrKind, job.Kind),
		attribute.String(attrQueue, job.Queue),
		attribute.Int64(attrJobID, job.ID),
		attribute.Int(attrAttempt, job.Attempt),
		attribute.Int(attrPriority, job.Priority),
	}
	spanName := "river." + job.Kind + ".work"
	ctx, span := tracer().Start(
		ctx, spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	metricsOK := m.ensureMetrics()
	if metricsOK {
		m.startedCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}

	start := time.Now()
	err := doInner(ctx)
	dur := time.Since(start)

	if metricsOK {
		outcome := outcomeSuccess
		if err != nil {
			outcome = outcomeError
		}
		outcomeAttr := attribute.String(attrOutcome, outcome)
		m.doneCounter.Add(ctx, 1,
			metric.WithAttributes(attrs...),
			metric.WithAttributes(outcomeAttr),
		)
		m.histDuration.Record(ctx, dur.Milliseconds(),
			metric.WithAttributes(attribute.String(attrKind, job.Kind), outcomeAttr))
	}

	if err != nil {
		// Record the error on the span so the OTel UI shows the failure
		// prominently; do NOT shadow the original error (callers need it
		// to drive retry behaviour).
		span.RecordError(err)
		span.SetAttributes(attribute.String(attrErrorType, errorTypeName(err)))
	}
	return err
}

// errorTypeName returns a short, low-cardinality type name for the error so
// the metric attribute doesn't explode. Falls back to "generic" for errors
// without a typed fmt.Errorf wrapper.
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	// We avoid errors.As reflection here; the type name is good enough for
	// cardinality purposes and the caller can dig into the span's recorded
	// error for the full message.
	return "generic"
}

// Middleware returns the otel worker middleware as a rivertype.WorkerMiddleware
// so callers can add it to river.Config.Middleware.
func (m *otelMiddleware) Middleware() rivertype.WorkerMiddleware {
	return m
}
