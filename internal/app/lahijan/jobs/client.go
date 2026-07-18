// Package jobs: client.go builds the River client on top of the existing
// pgxpool. The client is shared by every producer (services that queue work)
// and every consumer (the supervisor that runs the workers). One client per
// process is the recommended River pattern; multi-node deployments just run
// the same client against the shared Postgres (ADR-0009).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

// tx is the alias for the pgx.Tx type parameter River is generic over. We
// keep it as a private alias so callers don't need to import pgx just to
// hold a *Client.
type tx = pgx.Tx

// Client wraps a *river.Client[pgx.Tx] plus the registry used to build it,
// so the supervisor and admin API can reach both. The supervisor owns
// start/stop; the admin API uses JobGet/JobList/JobRetry/JobCancel.
type Client struct {
	river    *river.Client[tx]
	registry *Registry
	logger   *slog.Logger
}

// River returns the underlying *river.Client. The admin API reaches for it
// directly when it needs the full surface; producers should prefer the
// Insert helper on Client.
func (c *Client) River() *river.Client[tx] { return c.river }

// Registry returns the registry used to build this client. The admin UI uses
// it to enumerate kinds.
func (c *Client) Registry() *Registry { return c.registry }

// Logger returns the logger used by this client. Mostly for tests.
func (c *Client) Logger() *slog.Logger { return c.logger }

// Config carries the few process-wide knobs the River client needs.
type Config struct {
	// Logger receives River's internal logs. Defaults to slog.Default().
	Logger *slog.Logger

	// JobTimeout caps every Work() invocation. Defaults to 60s. A job can
	// override this via its worker's Timeout() method.
	JobTimeout time.Duration

	// MaxAttempts is the default retry budget for jobs that do not pin their
	// own. Defaults to 5 (matches River upstream).
	MaxAttempts int

	// PollOnly disables LISTEN/NOTIFY and falls back to polling — needed for
	// PgBouncer transaction-pooling deployments that do not pass through
	// Postgres notifications.
	PollOnly bool

	// Queues overrides the per-queue worker counts derived from the
	// registry. Keys are queue names; values are MaxWorkers. A queue not in
	// this map falls back to DefaultMaxWorkersPerQueue.
	Queues map[string]int

	// DefaultMaxWorkersPerQueue is the per-queue MaxWorkers when the queue
	// is not explicitly listed in Queues. Defaults to 10.
	DefaultMaxWorkersPerQueue int
}

// DefaultConfig returns a Config that matches River's recommended defaults
// for a small single-host deployment.
func DefaultConfig() Config {
	return Config{
		JobTimeout:                60 * time.Second,
		MaxAttempts:               5,
		DefaultMaxWorkersPerQueue: 10,
	}
}

// NewClient builds the River client from a pgxpool + a populated registry.
// The pool MUST already have River's schema applied (migration 0018); we do
// not run migrations here so the bootstrap path is identical in tests and
// production (migrations are wired by program.Start).
//
// The client does NOT start workers — call Supervisor.Start for that.
func NewClient(pool *pgxpool.Pool, registry *Registry, cfg Config) (*Client, error) {
	if pool == nil {
		return nil, errors.New("jobs: pool is nil")
	}
	if registry == nil {
		return nil, errors.New("jobs: registry is nil")
	}
	if registry.KindCount() == 0 {
		return nil, ErrEmptyRegistry
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	queues := buildQueues(registry, cfg)
	mw := newOtelMiddleware(cfg.Logger)
	riverCfg := &river.Config{
		Logger:      cfg.Logger,
		Workers:     registry.Workers(),
		Queues:      queues,
		JobTimeout:  cfg.JobTimeout,
		MaxAttempts: cfg.MaxAttempts,
		PollOnly:    cfg.PollOnly,
		Middleware: []rivertype.Middleware{
			mw,
		},
	}

	rc, err := river.NewClient(riverpgxv5.New(pool), riverCfg)
	if err != nil {
		return nil, fmt.Errorf("jobs: build river client: %w", err)
	}
	return &Client{river: rc, registry: registry, logger: cfg.Logger}, nil
}

// buildQueues derives the river.Queues map from the registry's specs +
// the supervisor overrides. Every distinct Queue mentioned in the registry
// gets an entry; the supervisor's Queues override can raise MaxWorkers.
func buildQueues(r *Registry, cfg Config) map[string]river.QueueConfig {
	defaults := cfg.DefaultMaxWorkersPerQueue
	if defaults < 1 {
		defaults = 10
	}
	out := make(map[string]river.QueueConfig, len(cfg.Queues)+1)
	// Always include the default queue so producers that target "default"
	// without registering a kind still round-trip.
	out[river.QueueDefault] = river.QueueConfig{MaxWorkers: defaults}
	for q, n := range cfg.Queues {
		if n < 1 {
			n = defaults
		}
		out[q] = river.QueueConfig{MaxWorkers: n}
	}
	for _, spec := range r.Kinds() {
		q := spec.Queue
		if q == "" {
			q = river.QueueDefault
		}
		if _, ok := out[q]; ok {
			continue
		}
		max := defaults
		if spec.Concurrency > 0 {
			max = spec.Concurrency
		}
		out[q] = river.QueueConfig{MaxWorkers: max}
	}
	return out
}

// Insert queues a job. Thin pass-through to river.Client.Insert; defined on
// Client so callers do not need to import river directly.
func (c *Client) Insert(
	ctx context.Context,
	args river.JobArgs,
	opts *river.InsertOpts,
) (*rivertype.JobInsertResult, error) {
	return c.river.Insert(ctx, args, opts)
}

// JobGet returns a job row by id. Wraps river.Client.JobGet; the admin API
// uses this for the "single job" endpoint.
func (c *Client) JobGet(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	return c.river.JobGet(ctx, id)
}

// JobList returns a page of jobs matching params. Wraps river.Client.JobList.
func (c *Client) JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error) {
	return c.river.JobList(ctx, params)
}

// JobRetry re-queues a discarded/DLQ'd job for an immediate retry. Wraps
// river.Client.JobRetry.
func (c *Client) JobRetry(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	return c.river.JobRetry(ctx, id)
}

// JobCancel cancels a queued or running job. Wraps river.Client.JobCancel.
func (c *Client) JobCancel(ctx context.Context, id int64) (*rivertype.JobRow, error) {
	return c.river.JobCancel(ctx, id)
}
