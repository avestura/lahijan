// Package jobs: registry.go holds the central registry of job kinds that this
// process can execute. Every worker that should run inside Lahijan registers
// itself here at bootstrap; the registry then drives:
//
//   - the river.Workers bundle passed to the River client (so that River
//     validates inserted jobs against a known kind and routes Work calls to
//     the right handler); and
//   - the admin UI's "what kinds exist" listing.
//
// Kinds are stable strings of the form "scope.action" (e.g.
// "billing.usage.rollup", "compute.instance.snapshot"). They NEVER mention
// Incus / PowerDNS / SeaweedFS per pillar 1.
package jobs

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/riverqueue/river"
)

// KindSpec describes a registered job kind. Concurrency, when non-zero,
// overrides the per-queue MaxWorkers for this kind (open question 1 in the
// WS-09 doc — default "yes, configurable via registry"). The queue name
// separates traffic into independent pools so a chatty kind cannot starve a
// quiet one. Tags are informational and used by the admin UI for grouping.
type KindSpec struct {
	// Kind is the stable "scope.action" identifier that producers pass to
	// river.Client.Insert as the args' Kind() value. Always populated.
	Kind string

	// Queue is the River queue this kind lands on. Empty defaults to the
	// River default queue ("default") at registration time.
	Queue string

	// Concurrency caps the number of in-flight workers for this kind across
	// the whole client. Zero means "follow the queue's MaxWorkers". This is
	// only an upper-bound hint for the supervisor when it builds the
	// per-queue worker pool; River enforces the actual cap.
	Concurrency int

	// Tags is an informational list used by the admin UI for grouping. Tags
	// do not affect scheduling.
	Tags []string

	// Description is a short human-friendly summary of what the kind does.
	// Surfaced in the admin UI.
	Description string
}

// Registry is the central catalog of job kinds known to this process. It
// owns the river.Workers bundle that the River client is built against;
// callers register workers via the generic Register function so the metadata
// and the worker implementation cannot drift.
//
// Safe for concurrent use: Register may be called from many goroutines (in
// practice it is only called from program.Start), and Kinds/SpecFor may be
// called concurrently once bootstrap is done.
type Registry struct {
	mu      sync.RWMutex
	kinds   map[string]KindSpec
	workers *river.Workers
}

// NewRegistry returns an empty Registry with a fresh internal river.Workers
// bundle. The bundle is shared with the River client built in NewClient.
func NewRegistry() *Registry {
	return &Registry{
		kinds:   make(map[string]KindSpec),
		workers: river.NewWorkers(),
	}
}

// Register adds a typed worker to the registry's Workers bundle and records
// its metadata. The kind is taken from args.Kind(); passing a spec whose Kind
// field is non-empty AND disagrees with args.Kind() panics (programming bug).
// Duplicate kinds panic too, mirroring river.AddWorker — this is a
// bootstrap-time programming bug and should fail loud and early.
//
// Generic in T so the compiler guarantees worker matches args at the call
// site; the supervisor uses the spec metadata to derive queue -> MaxWorkers.
func Register[T river.JobArgs](
	r *Registry,
	args T,
	worker river.Worker[T],
	spec KindSpec,
) {
	if r == nil {
		panic("jobs: Register: registry is nil")
	}
	kind := args.Kind()
	if spec.Kind != "" && spec.Kind != kind {
		panic(fmt.Sprintf(
			"jobs: register kind mismatch: spec %q != args.Kind() %q",
			spec.Kind, kind,
		))
	}
	spec.Kind = kind
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.kinds[kind]; ok {
		panic(fmt.Sprintf("jobs: kind %q already registered", kind))
	}
	river.AddWorker[T](r.workers, worker)
	r.kinds[kind] = spec
}

// Workers returns the river.Workers bundle the registry owns. The River
// client picks it up via Config.Workers.
func (r *Registry) Workers() *river.Workers { return r.workers }

// SpecFor returns the registered KindSpec for the given kind, or false if the
// kind is unknown. The admin UI uses this to render metadata.
func (r *Registry) SpecFor(kind string) (KindSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.kinds[kind]
	return s, ok
}

// Kinds returns every registered KindSpec sorted by Kind. Used by the admin
// UI to enumerate the catalog.
func (r *Registry) Kinds() []KindSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]KindSpec, 0, len(r.kinds))
	for _, s := range r.kinds {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// KindCount returns the number of registered kinds. Used by tests.
func (r *Registry) KindCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.kinds)
}

// ErrEmptyRegistry is returned by NewClient when no kind has been registered.
// A process that wants to consume jobs but registers nothing is almost
// certainly misconfigured.
var ErrEmptyRegistry = errors.New("jobs: registry is empty; register at least one worker before starting the client")
