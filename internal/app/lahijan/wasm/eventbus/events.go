// Package eventbus: events.go is the canonical registry of every event
// a provider (Phase 3) or module (Phase 4) emits. A typo at an emit call
// site surfaces as a topic nobody subscribes to; declaring them here as
// constants gives the compiler + the admin docs a single source of truth.
//
// Format mirrors audit actions + RBAC slugs: "scope.resource.verb" (e.g.
// "dns.record.created", "compute.instance.stopped"). The scope is the
// module; the resource is the entity within the module; the verb is the
// past-tense action. Per pillar 1, scopes are "dns"/"compute"/"s3" — never
// "powerdns"/"incus"/"seaweedfs".
//
// Metadata schemas for each event live in the module that emits them
// (Phase 3+); the registry here is just the topic constant.
package eventbus

// DNS module events (WS-12, WS-15). Emitted by the DNS service whenever
// a zone or record changes. Plugins subscribe via "dns.record.*" to get
// every record change in one stream. DNSSEC events fire when the per-zone
// signing toggle flips; key-rotation events fire when the operator
// rotates the zone-signing key (Phase 7 candidate, but the topics are
// part of the canonical registry from day one so the bus + docs agree).
const (
	DNSZoneCreated        = "dns.zone.created"
	DNSZoneUpdated        = "dns.zone.updated"
	DNSZoneDeleted        = "dns.zone.deleted"
	DNSZoneDNSECSecured   = "dns.zone.dnssec.enabled"
	DNSZoneDNSSECDisabled = "dns.zone.dnssec.disabled"
	DNSZoneDNSSECRotated  = "dns.zone.dnssec.rotated"
	DNSRecordCreated      = "dns.record.created"
	DNSRecordUpdated      = "dns.record.updated"
	DNSRecordDeleted      = "dns.record.deleted"
)

// Compute module events (WS-11, WS-14). Emitted by the compute service
// on lifecycle transitions. Plugins subscribe via "compute.instance.*"
// to drive autoscaler / notifier behaviour.
const (
	ComputeInstanceCreated   = "compute.instance.created"
	ComputeInstanceStarted   = "compute.instance.started"
	ComputeInstanceStopped   = "compute.instance.stopped"
	ComputeInstanceRestarted = "compute.instance.restarted"
	ComputeInstanceDeleted   = "compute.instance.deleted"
)

// Storage module events (WS-13, WS-16). Emitted by the storage service
// on bucket lifecycle. Plugins subscribe via "s3.bucket.*" for access
// auditing or cross-tenant replication.
const (
	S3BucketCreated = "s3.bucket.created"
	S3BucketDeleted = "s3.bucket.deleted"
	S3BucketUpdated = "s3.bucket.updated"
)

// Billing module events (WS-17). Emitted by the metering + ledger
// services. Plugins subscribe to drive low-balance alerts or wallet UIs.
const (
	BillingLowBalance = "billing.balance.low"
	BillingToppedUp   = "billing.balance.topped_up"
	BillingCharge     = "billing.balance.charged"
	BillingRefund     = "billing.balance.refunded"
)

// Plugin subsystem events (WS-10b/c). Emitted by the installer + the
// host-function framework. Plugins can subscribe to react to other
// plugins' lifecycles (e.g. a "kill switch" plugin that disables itself
// when a dependency goes away).
const (
	PluginInstalled = "plugin.installed"
	PluginEnabled   = "plugin.enabled"
	PluginDisabled  = "plugin.disabled"
	PluginDeleted   = "plugin.deleted"
)

// Auth module events. Emitted by the auth subsystem on identity lifecycle
// changes. Plugins subscribe to drive custom welcome flows or audit
// pipelines.
const (
	UserRegistered = "auth.user.registered"
	UserLogin      = "auth.user.login"
	UserLogout     = "auth.user.logout"
)

// AllEvents returns every canonical event topic. The admin "events"
// debug UI uses this to render the catalog; the manifest validator
// uses it to suggest topic patterns at install time.
func AllEvents() []string {
	return []string{
		DNSZoneCreated, DNSZoneUpdated, DNSZoneDeleted,
		DNSZoneDNSECSecured, DNSZoneDNSSECDisabled, DNSZoneDNSSECRotated,
		DNSRecordCreated, DNSRecordUpdated, DNSRecordDeleted,
		ComputeInstanceCreated, ComputeInstanceStarted, ComputeInstanceStopped,
		ComputeInstanceRestarted, ComputeInstanceDeleted,
		S3BucketCreated, S3BucketDeleted, S3BucketUpdated,
		BillingLowBalance, BillingToppedUp, BillingCharge, BillingRefund,
		PluginInstalled, PluginEnabled, PluginDisabled, PluginDeleted,
		UserRegistered, UserLogin, UserLogout,
	}
}

// IsKnownTopic reports whether the topic is one of the canonical events
// OR a dynamic plugin-emitted topic with the same shape. The bus uses
// this to warn (debug log) on unknown topics — a typo at an emit call
// site would otherwise silently produce an event nobody listens to.
//
// Dynamic topics ARE valid: a plugin can emit "my_plugin.tick" if it
// wants to coordinate with another instance of itself. The check is
// therefore: known OR matches "scope.resource.verb" pattern.
func IsKnownTopic(topic string) bool {
	for _, t := range AllEvents() {
		if t == topic {
			return true
		}
	}
	// Accept any "<scope>.<resource>.<verb>" shape; this is the
	// minimal syntactic check. Semantic validity is the caller's job.
	dots := 0
	for _, r := range topic {
		if r == '.' {
			dots++
		}
	}
	return dots >= 2
}
