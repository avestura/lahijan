// Package audit defines the audit-event seam every auth (and later every
// privileged) action emits through (pillar 7, WS-06 DoD). WS-08 wires the real
// RequirePerm-based enforcement and the MarkOutcome pattern that records the
// outcome of a privileged action after the side effect completes.
//
// The audit_log table is fully append-only (migration 0005); WS-08 adds a
// sibling audit_log_outcomes table (migration 0010) so the MarkOutcome trail
// is itself append-only. A typical privileged-action flow is:
//
//	auditID, _ := emitter.Emit(ctx, Event{Action: "compute.instance.create", Status: StatusPending})
//	defer func() { _ = emitter.MarkOutcome(ctx, auditID, status, details) }()
//	// ... perform the privileged action ...
//
// For fire-and-forget events (the WS-06 pattern), Emit alone with a final
// status is still sufficient; MarkOutcome is only needed when the caller wants
// to record what happened AFTER Emit ran.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Action constants. The auth subsystem actions live here (defined in WS-06);
// module-specific actions (compute, dns, s3, billing, plugins) live in the
// rbac permission registry (internal/app/lahijan/auth/rbac) and are mirrored
// here as i18n-keyed strings so the audit query API can render them.
//
// Format: "scope.action" (e.g. "auth.user.login", "compute.instance.create")
// per docs/glossary.md.
const (
	ActionRegister           = "auth.user.register"
	ActionLogin              = "auth.user.login"
	ActionLogout             = "auth.user.logout"
	ActionRefresh            = "auth.session.refresh"
	ActionRefreshReuse       = "auth.session.refresh_reuse"
	ActionVerifyEmail        = "auth.email.verify"
	ActionResendVerification = "auth.email.resend_verification"
	ActionPasswordResetReq   = "auth.password.reset_request"
	ActionPasswordResetConf  = "auth.password.reset_confirm"
	ActionEmailChangeReq     = "auth.email.change_request"
	ActionEmailChangeConf    = "auth.email.change_confirm"
	ActionPasswordChange     = "auth.password.change"
	ActionProfileUpdate      = "auth.user.profile_update"
	ActionPATCreate          = "auth.pat.create"
	ActionPATRevoke          = "auth.pat.revoke"
	ActionPATUse             = "auth.pat.use"

	// External IdP actions (WS-07a). Linked = a new (provider, subject)
	// row was bound to a user; Login = an existing identity was used to
	// log in; Unlink = a row was removed.
	ActionIdpLink   = "auth.idp.link"
	ActionIdpLogin  = "auth.idp.login"
	ActionIdpUnlink = "auth.idp.unlink"

	// MFA actions (WS-07c). Enroll covers every step of enrollment
	// (begin_registration / finish_registration / TOTP enroll);
	// Verify is a successful or failed factor verification at challenge
	// time; Disable is removing a factor; RecoveryRefresh is
	// regenerating the recovery code batch; Challenge is the login-time
	// challenge success; ChallengeFail covers a single failed challenge
	// attempt and the brute-force lockout.
	ActionMFAEnroll          = "auth.mfa.enroll"
	ActionMFAVerify          = "auth.mfa.verify"
	ActionMFADisable         = "auth.mfa.disable"
	ActionMFARecoveryRefresh = "auth.mfa.recovery_refresh"
	ActionMFAChallenge       = "auth.mfa.challenge"
	ActionMFAChallengeFail   = "auth.mfa.challenge_fail"

	// Audit subsystem actions (the audit query API itself emits these).
	ActionAuditExport = "audit.export"
	ActionRBACRoleOps = "rbac.role.update"

	// Platform-admin job system actions (WS-09). Emitted by the admin jobs
	// API when an admin retries or cancels a job. The audit row carries the
	// job_id, the pre- and post-action states, and the request_id so the
	// action is fully traceable.
	ActionPlatformJobRetry  = "platform.job.retry"
	ActionPlatformJobCancel = "platform.job.cancel"

	// Plugin lifecycle actions (WS-10a). Emitted by the installer.Service
	// on every state-changing privileged action. Each row carries the
	// plugin_id, the actor, and (where relevant) the permission slug or
	// status transition in metadata.
	ActionPluginUpload  = "plugins.upload"
	ActionPluginInstall = "plugins.install"
	ActionPluginGrant   = "plugins.grant"
	ActionPluginRevoke  = "plugins.revoke"
	ActionPluginEnable  = "plugins.enable"
	ActionPluginDisable = "plugins.disable"
	ActionPluginDelete  = "plugins.delete"

	// Object storage module actions (WS-16). Emitted by the storage service
	// on every state-changing privileged action across buckets +
	// credentials + quotas + presign. Each row carries the bucket_id (or
	// credential id), the actor, and the canonical bucket name + slug in
	// metadata so the audit query API can render a stable history.
	ActionS3BucketCreate     = "s3.bucket.create"
	ActionS3BucketUpdate     = "s3.bucket.update"
	ActionS3BucketDelete     = "s3.bucket.delete"
	ActionS3BucketQuotaSet   = "s3.bucket.quota.set"
	ActionS3CredentialMint   = "s3.credentials.create"
	ActionS3CredentialRevoke = "s3.credentials.revoke"
	ActionS3Presign          = "s3.presign"

	// Object storage lifecycle actions (WS-29, ADR-0036). Emitted by
	// the storage service on every state-changing privileged action
	// across versioning + lifecycle rules + object-lock. Each row
	// carries the bucket_id + the actor in metadata. The
	// s3.object.lifecycle_deleted action is emitted by the lifecycle
	// evaluator worker (storage.lifecycle.evaluate) with actor_type =
	// "system" + metadata.trigger = "lifecycle" so the audit query API
	// can distinguish lifecycle-driven deletes from user-driven ones.
	ActionS3BucketVersioningSet   = "s3.bucket.versioning.set"
	ActionS3BucketLifecycleSet    = "s3.bucket.lifecycle.set"
	ActionS3BucketObjectLockSet   = "s3.bucket.object_lock.set"
	ActionS3ObjectLifecycleDelete = "s3.object.lifecycle_deleted"

	// Billing & metering module actions (WS-17). Emitted by the billing
	// service on every state-changing privileged admin action across
	// topups, refunds, and price catalog changes. Each row carries the
	// user_id (the user being credited/debited), the actor, and the
	// amount + reference in metadata so the audit query API can render a
	// stable history.
	ActionBillingTopup        = "billing.balance.topup"
	ActionBillingRefund       = "billing.balance.refund"
	ActionBillingPriceUpsert  = "billing.price.upsert"
	ActionBillingPriceExpire  = "billing.price.expire"
	ActionBillingReceiptGen   = "billing.receipt.generate"
	ActionBillingForceRebuild = "billing.balance.rebuild"

	// Billing payments + subscriptions actions (WS-27, ADR-0034).
	// Emitted by the payments service on every state-changing action
	// across plans, payment methods, subscriptions, promo codes, and
	// webhook ingestion. Each row carries the relevant ids + the actor
	// in metadata so the audit query API can render a stable history.
	ActionBillingPlanCreate         = "billing.plan.create"
	ActionBillingPlanUpdate         = "billing.plan.update"
	ActionBillingPlanDelete         = "billing.plan.delete"
	ActionBillingPaymentMethodAdd   = "billing.payment_method.add"
	ActionBillingPaymentMethodDrop  = "billing.payment_method.drop"
	ActionBillingSubscriptionCreate = "billing.subscription.create"
	ActionBillingSubscriptionCancel = "billing.subscription.cancel"
	ActionBillingPromoCodeCreate    = "billing.promo_code.create"
	ActionBillingPromoCodeRevoke    = "billing.promo_code.revoke"
	ActionBillingPromoCodeRedeem    = "billing.promo_code.redeem"
	ActionBillingWebhookReceived    = "billing.webhook.received"
	ActionBillingWebhookApplied     = "billing.webhook.applied"

	// DNS domain (registrar resale) actions (WS-28). Emitted by the
	// registrar service on every state-changing privileged action
	// across search / register / renew / transfer / DNSSEC. Each row
	// carries the domain name + the registrar order id in metadata so
	// the audit query API can render a stable history.
	ActionDNSDomainSearch   = "dns.domain.search"
	ActionDNSDomainRegister = "dns.domain.register"
	ActionDNSDomainRenew    = "dns.domain.renew"
	ActionDNSDomainTransfer = "dns.domain.transfer"
	ActionDNSDomainDelete   = "dns.domain.delete"
	ActionDNSDomainDNSSEC   = "dns.domain.dnssec"
)

// Standard statuses recorded on audit_log.status and audit_log_outcomes.status.
// Pending is used when Emit opens a row before a side effect and MarkOutcome
// finalizes it; success/failure are the terminal states.
const (
	StatusPending = "pending"
	StatusSuccess = "success"
	StatusFailure = "failure"
)

// Actor types recorded on audit_log.actor_type.
const (
	ActorUser   = "user"
	ActorSystem = "system"
	ActorPlugin = "plugin"
)

// ResourceType constants. The auth subsystem resources live here; module
// resources (instance, zone, bucket, ...) live in their own packages and are
// passed in as strings when those modules ship.
const (
	ResourceUser    = "user"
	ResourceSession = "session"
	ResourcePAT     = "personal_access_token"
	ResourceEmail   = "email"
	ResourceAudit   = "audit_log"
	ResourceRole    = "role"
	ResourceTenant  = "tenant"
	ResourceJob     = "job"
	ResourcePlugin  = "plugin"
	// ResourceBucket / ResourceCredential are the object-storage resource
	// types (WS-16). Emitted by the storage service.
	ResourceBucket     = "storage_bucket"
	ResourceCredential = "storage_credential"

	// ResourceLifecycleRule is the lifecycle-rule resource type (WS-29,
	// ADR-0036). Emitted by the storage service on lifecycle-rule
	// privileged actions.
	ResourceLifecycleRule = "storage_lifecycle_rule"

	// Billing resource types (WS-17). Emitted by the billing service.
	ResourceLedgerEntry = "ledger_entry"
	ResourcePrice       = "price"
	ResourceReceipt     = "receipt"
	ResourceBalance     = "balance"

	// Billing payments + subscriptions resource types (WS-27).
	ResourceBillingPlan          = "billing_plan"
	ResourceBillingPaymentMethod = "billing_payment_method"
	ResourceBillingSubscription  = "billing_subscription"
	ResourceBillingPromoCode     = "billing_promo_code"
	ResourceBillingWebhookEvent  = "billing_webhook_event"

	// DNS domain resource type (WS-28 registrar resale). Emitted by
	// the registrar service.
	ResourceDNSDomain = "dns_domain"
)

// Event is the data an emitter records. TenantID is nil for system-level auth
// events (login is global); Metadata is a free-form JSON blob for details that
// don't deserve their own column.
type Event struct {
	TenantID     *uuid.UUID
	ActorUserID  *uuid.UUID
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Status       string
	RequestID    *string
	Metadata     map[string]any
}

// Outcome carries the data for a MarkOutcome call. Details is a free-form JSON
// blob (typically error context for failures).
type Outcome struct {
	Status  string
	Details map[string]any
}

// Emitter records audit events and their outcomes. Implementations must be
// safe for concurrent use. Emit and MarkOutcome must never block the caller
// indefinitely: a failing write is logged but must not roll back the audited
// action. Both methods append rows; nothing ever updates or deletes them.
type Emitter interface {
	// Emit writes a row to audit_log and returns its id so the caller can
	// pass it to MarkOutcome. Callers that don't need the outcome trail can
	// discard the id.
	Emit(ctx context.Context, event Event) (uuid.UUID, error)

	// MarkOutcome appends a row to audit_log_outcomes for the given audit
	// id, recording the post-side-effect status. Safe to call multiple times
	// for the same audit id (the latest row wins as the "current" status).
	// Returns an error if the audit id does not exist or the write fails.
	MarkOutcome(ctx context.Context, auditID uuid.UUID, outcome Outcome) error
}

// NoopEmitter discards every event. Used in tests that don't assert on audit
// rows and in dev runs where the DB is unavailable.
type NoopEmitter struct{}

// Emit implements Emitter by doing nothing and returning the nil UUID.
func (NoopEmitter) Emit(_ context.Context, _ Event) (uuid.UUID, error) {
	return uuid.Nil, nil
}

// MarkOutcome implements Emitter by doing nothing.
func (NoopEmitter) MarkOutcome(_ context.Context, _ uuid.UUID, _ Outcome) error {
	return nil
}

// DBEmitter persists events to the audit_log table via AuditLogRepository and
// outcomes to audit_log_outcomes. Both tables are append-only by trigger, so
// this emitter is the only sanctioned mutating path either table can hit.
type DBEmitter struct {
	repo *database.AuditLogRepository
}

// NewDBEmitter wraps an AuditLogRepository.
func NewDBEmitter(repo *database.AuditLogRepository) *DBEmitter {
	return &DBEmitter{repo: repo}
}

// Emit writes the event to the audit_log table and returns the new row id.
func (e *DBEmitter) Emit(ctx context.Context, ev Event) (uuid.UUID, error) {
	status := ev.Status
	if status == "" {
		status = StatusSuccess
	}
	actorType := ev.ActorType
	if actorType == "" {
		actorType = ActorUser
	}
	var meta json.RawMessage
	if ev.Metadata != nil {
		raw, err := json.Marshal(ev.Metadata)
		if err != nil {
			return uuid.Nil, fmt.Errorf("audit: marshal metadata: %w", err)
		}
		meta = raw
	}
	row, err := e.repo.Create(ctx, database.CreateAuditLogParams{
		TenantID:     ev.TenantID,
		ActorUserID:  ev.ActorUserID,
		ActorType:    actorType,
		Action:       ev.Action,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		Status:       &status,
		RequestID:    ev.RequestID,
		Metadata:     meta,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: emit %s: %w", ev.Action, err)
	}
	return row.ID, nil
}

// MarkOutcome appends a row to audit_log_outcomes for the given audit id.
func (e *DBEmitter) MarkOutcome(ctx context.Context, auditID uuid.UUID, outcome Outcome) error {
	status := outcome.Status
	if status == "" {
		status = StatusSuccess
	}
	var details json.RawMessage
	if outcome.Details != nil {
		raw, err := json.Marshal(outcome.Details)
		if err != nil {
			return fmt.Errorf("audit: marshal outcome details: %w", err)
		}
		details = raw
	}
	if err := e.repo.MarkOutcome(ctx, auditID, status, details); err != nil {
		return fmt.Errorf("audit: mark outcome %s: %w", auditID, err)
	}
	return nil
}
