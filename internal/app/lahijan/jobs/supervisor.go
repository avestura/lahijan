// Package jobs: supervisor.go owns the River client's lifecycle (Start /
// Stop), wired into program.Start so the process shuts down cleanly on
// SIGTERM. The supervisor also exposes a "ready" signal so the readiness
// probe can wait for the queue maintenance services to elect a leader before
// accepting traffic.
//
// The supervisor does not own the workers — those live in the registry. The
// supervisor only owns the start/stop ordering: Start the River client after
// migrations are applied, Stop it before the pgxpool is closed.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Supervisor owns the River client's start/stop lifecycle. It is safe to
// construct once at bootstrap and call Start/Stop exactly once each.
type Supervisor struct {
	client *Client
	logger *slog.Logger

	// softStopTimeout caps how long we wait for in-flight jobs to finish
	// during Stop before escalating to a hard cancel (River's
	// StopAndCancel). Default 30s; override via Config.
	softStopTimeout time.Duration

	mu      sync.Mutex
	started bool
	stopped bool
}

// NewSupervisor wraps a Client. The Client must already be built (its
// registry populated) — the supervisor does not mutate it.
func NewSupervisor(client *Client, softStopTimeout time.Duration) (*Supervisor, error) {
	if client == nil {
		return nil, errors.New("jobs: supervisor: client is nil")
	}
	if softStopTimeout <= 0 {
		softStopTimeout = 30 * time.Second
	}
	return &Supervisor{
		client:          client,
		logger:          client.Logger(),
		softStopTimeout: softStopTimeout,
	}, nil
}

// Start starts the River client's workers + maintenance services. It is
// idempotent — a second call returns nil without re-starting. The caller
// owns the supplied ctx: cancelling it does NOT stop the workers; call
// Stop to drain them gracefully.
//
// Returns when the client has finished starting (typically <1s); workers
// keep running in background goroutines River owns.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	s.logger.Info("jobs: starting river supervisor")
	if err := s.client.River().Start(ctx); err != nil {
		return errors.Join(errors.New("jobs: river start"), err)
	}
	s.logger.Info("jobs: river supervisor started")
	return nil
}

// Stop drains the queue: signals every worker to stop picking up new jobs,
// waits up to SoftStopTimeout for in-flight jobs to finish, then escalates to
// a hard cancel. Idempotent — a second call returns nil immediately.
//
// Cancelling the supplied ctx shortens the soft-stop window: River escalates
// to a hard cancel as soon as ctx is done.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	s.mu.Unlock()

	s.logger.Info("jobs: stopping river supervisor", "soft_stop_timeout", s.softStopTimeout)
	stopCtx, cancel := context.WithTimeout(ctx, s.softStopTimeout)
	defer cancel()
	if err := s.client.River().Stop(stopCtx); err != nil {
		s.logger.Warn("jobs: river soft stop failed; escalating to cancel", "error", err)
		// Fall back to a hard cancel so the process can still exit.
		hardCtx, hardCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer hardCancel()
		if err2 := s.client.River().StopAndCancel(hardCtx); err2 != nil {
			return errors.Join(
				errors.New("jobs: river stop"),
				err,
				errors.New("jobs: river stop-and-cancel"),
				err2,
			)
		}
		return errors.Join(errors.New("jobs: river stop"), err)
	}
	s.logger.Info("jobs: river supervisor stopped")
	return nil
}

// Stopped returns a channel that closes when the supervisor has fully
// stopped. Useful for orchestration that needs to wait for the queue to
// drain before closing the DB pool.
func (s *Supervisor) Stopped() <-chan struct{} {
	return s.client.River().Stopped()
}
