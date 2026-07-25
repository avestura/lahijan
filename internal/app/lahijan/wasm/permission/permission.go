// Package permission defines the slug catalog, the prefix-match algorithm,
// and the enforcer seam the WASM host functions call before doing anything
// on behalf of a plugin (WS-10a).
//
// Slugs follow the same "scope.action" format as RBAC
// (internal/app/lahijan/auth/rbac), plus an optional ":qualifier" suffix
// for resource-scoped capabilities. Qualifiers may end with `*` to grant
// prefix-matching breadth:
//
//	kv.read:cache            — exact: only "kv.read:cache" matches.
//	events.listen:dns.record.* — prefix: every qualifier under
//	                              "dns.record." matches (created, deleted,
//	                              updated, ...).
//	kv.read:*                — every read on every kv namespace.
//
// Catalog. Every slug the host functions expose is declared in this package
// (see allCapabilities). The admin permission-approval UI reads from the
// catalog so an admin never sees a typo'd slug.
//
// Enforcer. Two implementations live here:
//
//   - DBEnforcer — reads from the plugin_permissions table via
//     *database.PluginsRepository. Used in production.
//   - MapEnforcer — reads from an in-memory map. Used in tests + the
//     no-DB dev mode.
//
// Host functions in WS-10b will call Enforcer.Allowed(ctx, pluginID, slug)
// before performing the privileged action. A denied call returns
// ErrPermissionDenied which the host function translates to a WASM trap.
package permission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// ErrPermissionDenied is returned by Enforcer.Allowed when the plugin does
// not hold the requested permission. Host functions translate it to a WASM
// trap; the API translates it to a 403 envelope.
var ErrPermissionDenied = errors.New("permission: denied")

// ErrUnknownPermission is returned when Validate rejects a slug that is not
// in the catalog AND does not match a known wildcard shape.
var ErrUnknownPermission = errors.New("permission: unknown slug")

// Slug constants for every capability a plugin can request. Host functions
// in WS-10b will reference these; the manifest parser (wasm/manifest)
// validates against the catalog via Validate.
//
// Slugs are grouped by the host-function family that will gate them.
// New capabilities must be added here AND in the manifest schema docs.
const (
	// Network: outbound HTTP fetch. Wildcards by host are not supported in
	// MVP — a single grant covers any URL the plugin fetches through the
	// host function. Per-host scoping is a Phase 7 candidate.
	CapNetworkOutbound = "network.outbound"

	// KV: per-plugin namespace read/write. The qualifier is the namespace
	// name; "kv.read:cache" allows reads from the "cache" namespace.
	// "kv.read:*" allows reads from any namespace the host function exposes.
	CapKVRead  = "kv.read"
	CapKVWrite = "kv.write"

	// Events: emit / listen on the in-process event bus (WS-10b). The
	// listen qualifier is the topic; "events.listen:dns.record.*" matches
	// every dns.record.* topic.
	CapEventsEmit   = "events.emit"
	CapEventsListen = "events.listen"

	// Jobs: schedule work on the River queue (WS-09). A plugin that needs
	// background work declares this; the host function injects the job
	// under the plugin's identity so the audit trail ties back.
	CapJobSchedule = "job.schedule"

	// HTTP handler: register an HTTP route under /api/v1/plugins/<name>/.
	// The qualifier is the sub-path ("/foo"); "api.handler.register:/foo"
	// allows the plugin to register exactly that route. The wildcard
	// "api.handler.register:*" allows any sub-path.
	CapAPIHandlerRegister = "api.handler.register"

	// Config: read the plugin's own config from the manifest's config
	// schema. The qualifier is the plugin's own name (so a plugin cannot
	// read another plugin's config).
	CapConfigRead = "config.read"

	// WASI (WS-10e / ADR-0039): Preview 1 capabilities for plugins
	// compiled to wasm32-wasi. The filtered WASI importer reads these
	// grants to configure wazero's FSConfig + env vars per plugin.

	// CapWasifSPreopen gates filesystem preopens. The qualifier is the
	// guest path with an optional :ro / :rw mode suffix
	// (e.g. "wasi.fs.preopen:/data:rw"). The host mounts
	// <fs_root>/<plugin-slug>/<host_subdir> at the guest path.
	CapWasifSPreopen = "wasi.fs.preopen"

	// CapWasiEnv gates environment variable visibility. The qualifier is
	// the env var name (e.g. "wasi.env:API_ENDPOINT"). The value is
	// injected by the admin via the plugin config endpoint; the plugin
	// never sees the host's actual env.
	CapWasiEnv = "wasi.env"

	// CapWasiClock gates clock_time_get / clock_res_get.
	CapWasiClock = "wasi.clock"

	// CapWasiRandom gates random_get. Low-risk; usually default-granted.
	CapWasiRandom = "wasi.random"

	// CapWasiExit gates proc_exit.
	CapWasiExit = "wasi.exit"

	// CapWasiWildcard is the god-mode escape hatch for WASI plugins.
	// When granted, the runtime registers the full
	// wasi_snapshot_preview1 surface with no filtering. The admin UI
	// shows a loud warning. This does NOT grant Lahijan host-function
	// permissions (kv, events, etc.) — those still need explicit grants.
	CapWasiWildcard = "*"

	// Compute instance operations (WS-10f). Plugins that manage
	// infrastructure declare these; the host function delegates to the
	// compute service which enforces RBAC + tenant scoping + billing.
	CapComputeInstanceCreate  = "compute.instance.create"
	CapComputeInstanceRead    = "compute.instance.read"
	CapComputeInstanceUpdate  = "compute.instance.update"
	CapComputeInstanceDelete  = "compute.instance.delete"
	CapComputeInstanceControl = "compute.instance.control"

	// DNS zone operations (WS-10f).
	CapDNSZoneCreate = "dns.zone.create"
	CapDNSZoneRead   = "dns.zone.read"
	CapDNSZoneDelete = "dns.zone.delete"

	// DNS record operations (WS-10f).
	CapDNSRecordCreate = "dns.record.create"
	CapDNSRecordRead   = "dns.record.read"
	CapDNSRecordDelete = "dns.record.delete"

	// Storage bucket operations (WS-10f).
	CapStorageBucketCreate = "storage.bucket.create"
	CapStorageBucketRead   = "storage.bucket.read"
	CapStorageBucketDelete = "storage.bucket.delete"
)

// allCapabilities is the single source of truth for what a plugin can
// request. The admin UI and the manifest validator both derive their lists
// from this. New capabilities land here first.
//
// Order is grouped by host-function family for readability.
var allCapabilities = []string{
	CapNetworkOutbound,
	CapKVRead,
	CapKVWrite,
	CapEventsEmit,
	CapEventsListen,
	CapJobSchedule,
	CapAPIHandlerRegister,
	CapConfigRead,
	CapWasifSPreopen,
	CapWasiEnv,
	CapWasiClock,
	CapWasiRandom,
	CapWasiExit,
	CapComputeInstanceCreate,
	CapComputeInstanceRead,
	CapComputeInstanceUpdate,
	CapComputeInstanceDelete,
	CapComputeInstanceControl,
	CapDNSZoneCreate,
	CapDNSZoneRead,
	CapDNSZoneDelete,
	CapDNSRecordCreate,
	CapDNSRecordRead,
	CapDNSRecordDelete,
	CapStorageBucketCreate,
	CapStorageBucketRead,
	CapStorageBucketDelete,
}

// AllCapabilities returns a copy of the catalog. Callers must not mutate
// the returned slice.
func AllCapabilities() []string {
	out := make([]string, len(allCapabilities))
	copy(out, allCapabilities)
	return out
}

// IsKnownCapability reports whether the slug's scope.action prefix (the part
// before ":") is in the catalog. Used by the manifest validator to reject
// typos at install time. Qualifiers (the part after ":") are not checked
// here because their semantics are host-function-specific.
//
// The "*" wildcard (CapWasiWildcard) is always recognised — it is the
// WASI god-mode escape hatch (ADR-0039) and bypasses the normal
// scope.action format check.
func IsKnownCapability(slug string) bool {
	if slug == CapWasiWildcard {
		return true
	}
	prefix := strings.SplitN(slug, ":", 2)[0]
	for _, c := range allCapabilities {
		if c == prefix {
			return true
		}
	}
	return false
}

// Validate checks that a slug is syntactically valid (scope.action[:qual])
// and that the scope.action prefix is in the catalog. Returns nil on success
// or one of ErrUnknownPermission / a wrapping error.
//
// The "*" wildcard (CapWasiWildcard) is always valid — it is the WASI
// god-mode escape hatch (ADR-0039) and does not follow the scope.action
// format.
func Validate(slug string) error {
	if slug == CapWasiWildcard {
		return nil
	}
	if slug == "" {
		return fmt.Errorf("permission: empty slug: %w", ErrUnknownPermission)
	}
	prefix := strings.SplitN(slug, ":", 2)[0]
	if !strings.Contains(prefix, ".") {
		return fmt.Errorf("permission: %q must be scope.action[:qual]: %w",
			slug, ErrUnknownPermission)
	}
	if !IsKnownCapability(slug) {
		return fmt.Errorf("permission: %q is not in the capability catalog: %w",
			slug, ErrUnknownPermission)
	}
	// Qualifier (the part after ":") must be non-empty and not whitespace.
	if idx := strings.Index(slug, ":"); idx >= 0 {
		qual := slug[idx+1:]
		if strings.TrimSpace(qual) == "" {
			return fmt.Errorf("permission: %q has empty qualifier: %w",
				slug, ErrUnknownPermission)
		}
	}
	return nil
}

// Allowed reports whether the requested slug matches any of the granted
// slugs. Matching is:
//   - exact string match, OR
//   - granted slug ends with ":*" (a "scope.action:*" wildcard) AND the
//     requested slug is the same scope.action with a non-empty qualifier, OR
//   - granted slug ends with ".*" AND the part before the "*" contains a
//     qualifier separator (":") AND the requested slug starts with the
//     granted prefix.
//
// Scope-level wildcards ("scope.*") are deliberately NOT supported — every
// capability is declared explicitly in the catalog so a typo cannot grant
// unintended scope-wide breadth.
//
// Examples:
//
//	granted "kv.read:cache", requested "kv.read:cache"        -> true
//	granted "kv.read:*",     requested "kv.read:cache"        -> true
//	granted "kv.read:*",     requested "kv.write:cache"       -> false (different action)
//	granted "events.listen:dns.record.*", requested
//	        "events.listen:dns.record.created"                -> true
//	granted "events.listen:dns.record.*", requested
//	        "events.listen:dns.zone.created"                  -> false
//	granted "kv.*",          requested "kv.read:cache"        -> false (scope wildcard not supported)
//	granted "kv.read:rec*",  requested "kv.read:record"       -> false (mid-token "*" not supported)
func Allowed(granted []string, requested string) bool {
	if requested == "" {
		return false
	}
	for _, g := range granted {
		if g == requested {
			return true
		}
		// "scope.action:*" — any qualifier on this action.
		if strings.HasSuffix(g, ":*") {
			prefix := strings.TrimSuffix(g, "*") // "scope.action:"
			// Requested must be "<prefix><non-empty qualifier>".
			if strings.HasPrefix(requested, prefix) && len(requested) > len(prefix) {
				return true
			}
			continue
		}
		// "scope.action:foo.*" — qualifier sub-path wildcard. The "." must
		// come after a ":", otherwise this is a scope-level wildcard
		// ("scope.*") which we deliberately do not support.
		if strings.HasSuffix(g, ".*") {
			head := strings.TrimSuffix(g, "*") // "scope.action:foo."
			if !strings.Contains(head, ":") {
				continue
			}
			if strings.HasPrefix(requested, head) && len(requested) > len(head) {
				return true
			}
			continue
		}
	}
	return false
}

// Enforcer answers one question: "may plugin P perform action A?" Host
// functions call this before doing anything privileged; a false (or any
// error) MUST fail closed.
//
// Implementations MUST be safe for concurrent use.
type Enforcer interface {
	// Allowed returns true when the plugin holds a grant (exact or wildcard)
	// for the slug. A failure to reach the DB (or any unexpected error)
	// returns (false, error); callers MUST fail closed by treating the
	// error as "deny" and trapping the plugin.
	Allowed(ctx context.Context, pluginID uuid.UUID, slug string) (bool, error)

	// ListGrants returns the full set of permission slugs the plugin holds.
	// Used by the WASI importer to derive the filesystem + env configuration
	// from granted slugs.
	ListGrants(ctx context.Context, pluginID uuid.UUID) ([]string, error)
}

// DBEnforcer reads grants from the plugin_permissions table. Construct one
// at bootstrap and share it across requests.
type DBEnforcer struct {
	repo *database.PluginsRepository
}

// NewDBEnforcer wraps a PluginsRepository.
func NewDBEnforcer(repo *database.PluginsRepository) *DBEnforcer {
	return &DBEnforcer{repo: repo}
}

// Allowed implements Enforcer by listing every grant the plugin holds and
// running the prefix-match algorithm. We could short-circuit on an exact
// match via HasExactGrant, but the common case is "plugin has 1-3 grants"
// and the extra round-trip is more expensive than the in-memory scan.
func (e *DBEnforcer) Allowed(ctx context.Context, pluginID uuid.UUID, slug string) (bool, error) {
	grants, err := e.repo.ListPermissions(ctx, pluginID)
	if err != nil {
		return false, fmt.Errorf("permission: list grants for %s: %w", pluginID, err)
	}
	strs := make([]string, 0, len(grants))
	for _, g := range grants {
		strs = append(strs, g.Permission)
	}
	return Allowed(strs, slug), nil
}

// ListGrants implements Enforcer by listing every grant the plugin holds.
func (e *DBEnforcer) ListGrants(ctx context.Context, pluginID uuid.UUID) ([]string, error) {
	grants, err := e.repo.ListPermissions(ctx, pluginID)
	if err != nil {
		return nil, fmt.Errorf("permission: list grants for %s: %w", pluginID, err)
	}
	strs := make([]string, len(grants))
	for i, g := range grants {
		strs[i] = g.Permission
	}
	return strs, nil
}
// The map is keyed by plugin id; the value is the set of granted slugs.
// Safe for concurrent use.
type MapEnforcer struct {
	mu  sync.RWMutex
	set map[uuid.UUID][]string
}

// NewMapEnforcer builds an empty MapEnforcer.
func NewMapEnforcer() *MapEnforcer {
	return &MapEnforcer{set: make(map[uuid.UUID][]string)}
}

// Grant records a slug for the plugin (idempotent).
func (e *MapEnforcer) Grant(pluginID uuid.UUID, slugs ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	existing := e.set[pluginID]
	for _, s := range slugs {
		dup := false
		for _, g := range existing {
			if g == s {
				dup = true
				break
			}
		}
		if !dup {
			existing = append(existing, s)
		}
	}
	e.set[pluginID] = existing
}

// Revoke removes a slug from the plugin's grant set.
func (e *MapEnforcer) Revoke(pluginID uuid.UUID, slug string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	existing := e.set[pluginID]
	out := existing[:0]
	for _, g := range existing {
		if g != slug {
			out = append(out, g)
		}
	}
	e.set[pluginID] = out
}

// Allowed implements Enforcer.
func (e *MapEnforcer) Allowed(_ context.Context, pluginID uuid.UUID, slug string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Allowed(e.set[pluginID], slug), nil
}

// ListGrants implements Enforcer by returning a copy of the plugin's grant set.
func (e *MapEnforcer) ListGrants(_ context.Context, pluginID uuid.UUID) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]string(nil), e.set[pluginID]...), nil
}
