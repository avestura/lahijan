// Package dns implements Lahijan's user-facing DNS module (WS-15). It
// orchestrates calls to the PowerDNS provider driver (Phase 3, WS-12),
// the tenant-scoped dns_zones + dns_records tables (this WS), the RBAC
// policy + audit emitter (WS-08), and the WASM event bus (WS-10b).
//
// Layering:
//
//	api/dns_handlers.go  -> dns.Service -> providers/powerdns + database
//	                                     \-> auth/audit
//	                                     \-> wasm/eventbus
//
// Every privileged action calls RequirePerm via the api/middleware gate;
// every state-changing action emits an audit event before the side effect
// (status=pending) and marks the outcome after. Every zone/record change
// also emits into the WASM event bus so plugins can react.
//
// Per pillar 1, this package is named "dns" — never "powerdns". End users
// do not see the word PowerDNS anywhere in the API or UI.
package dns

import (
	"context"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// ResourceZone / ResourceRecord are the audit resource_type values for
// the DNS module.
const (
	ResourceZone   = "dns_zone"
	ResourceRecord = "dns_record"
)

// AuditAction constants for DNS. Past-tense verbs so the audit row reads
// "what happened"; i18n keys are auto-derived (dots -> underscores).
const (
	AuditZoneCreate = "dns.zone.create"
	AuditZoneUpdate = "dns.zone.update"
	AuditZoneDelete = "dns.zone.delete"

	AuditRecordCreate = "dns.record.create"
	AuditRecordUpdate = "dns.record.update"
	AuditRecordDelete = "dns.record.delete"

	AuditDNSSECEnable  = "dns.zone.dnssec.enable"
	AuditDNSSECDisable = "dns.zone.dnssec.disable"

	AuditTemplateApply = "dns.zone.template.apply"
)

// Service is the entrypoint every DNS API handler talks to. It owns the
// PowerDNS provider (Phase 3 driver), the tenant-scoped repositories, the
// audit emitter, the WASM event bus, and the RBAC policy evaluator. Every
// method takes a context carrying the tenant id (set by the tenant
// middleware) and the user id (set by the auth middleware); the service
// enforces RBAC at the api/middleware layer for HTTP requests and via
// rbac.Require for non-HTTP entry points (jobs, websocket-driven flows).
type Service struct {
	provider pdnsProvider
	repos    *database.Repos
	audit    audit.Emitter
	bus      eventBus
	policy   rbac.PolicyEvaluator
	config   Config
}

// pdnsProvider is the narrow seam the service needs from
// providers/powerdns.Provider. Defined here so tests can swap a fake
// without dragging the full powerdns surface into the test file. Split
// into per-area sub-interfaces so the surface stays reviewable; the
// concrete *powerdns.Provider satisfies the union.
type pdnsProvider interface {
	pdnsZoneOps
	pdnsRecordOps
	pdnsDNSSECOps
}

// pdnsZoneOps covers zone CRUD + health.
type pdnsZoneOps interface {
	Ping(ctx context.Context) error
	CreateZone(ctx context.Context, params powerdns.CreateZoneParams) (*powerdns.Zone, error)
	GetZone(ctx context.Context, zoneID string, rrsets bool) (*powerdns.Zone, error)
	ListZones(ctx context.Context) ([]powerdns.Zone, error)
	UpdateZone(ctx context.Context, zoneID string, update powerdns.ZoneUpdate) error
	DeleteZone(ctx context.Context, zoneID string) error
}

// pdnsRecordOps covers RRset REPLACE/DELETE + bulk fetch.
type pdnsRecordOps interface {
	ReplaceRRset(ctx context.Context, params powerdns.RRsetUpsertParams) error
	DeleteRRset(ctx context.Context, params powerdns.DeleteRRsetParams) error
	SearchRRsets(
		ctx context.Context,
		zoneID string,
		nameFilter string,
		typeFilter powerdns.RecordType,
	) ([]powerdns.RRset, error)
}

// pdnsDNSSECOps covers DNSSEC toggle + key management.
type pdnsDNSSECOps interface {
	EnableDNSSEC(ctx context.Context, zoneID string) (*powerdns.CryptoKey, error)
	DisableDNSSEC(ctx context.Context, zoneID string) error
	IsDNSSECEnabled(ctx context.Context, zoneID string) (bool, error)
	ListCryptoKeys(ctx context.Context, zoneID string) ([]powerdns.CryptoKey, error)
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// Config carries the few process-wide knobs the service needs.
type Config struct {
	// DefaultNameservers is the list of NS target names written into the
	// initial SOA + NS RRsets at zone-create time. Each entry must be a
	// canonical name with a trailing dot. Required for create-zone to
	// succeed against PDNS; the program layer wires this from
	// conf.providers.powerdns.default_nameservers.
	DefaultNameservers []string

	// DefaultTTL is the TTL applied when the caller does not supply one.
	// Clamped to [MinTTL, MaxTTL] (validators.go).
	DefaultTTL int

	// DefaultDNSSECEnabled controls whether new zones get DNSSEC turned
	// on at create time. Off by default per WS-12 "Open questions"
	// item 1; the admin flips it per-zone via EnableDNSSEC.
	DefaultDNSSECEnabled bool

	// MaxRecordsPerTenant caps the total records a tenant can hold across
	// every zone. A zero value disables the cap (useful for tests).
	MaxRecordsPerTenant int
}

// New builds a Service. Every dependency is required except `bus` and
// `policy` (nil disables event emission / non-HTTP RequirePerm).
func New(
	provider pdnsProvider,
	r *database.Repos,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	cfg Config,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	ttl := cfg.DefaultTTL
	if ttl == 0 {
		ttl = 3600
	}
	cfg.DefaultTTL = ttl
	return &Service{
		provider: provider,
		repos:    r,
		audit:    emitter,
		bus:      bus,
		policy:   policy,
		config:   cfg,
	}
}
