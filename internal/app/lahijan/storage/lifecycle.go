// Package storage: lifecycle.go implements the per-bucket lifecycle-rule
// privileged actions on the storage.Service. The lifecycle rule set is
// the source-of-truth on the Postgres side (per ADR-0036 sub-decision
// A); every Set / Update / Delete pushes the resulting rule set to
// SeaweedFS via the provider's SetBucketLifecycle so the daemon honours
// the policy natively when it can. The lifecycle evaluator River
// worker (storage.lifecycle.evaluate) ALSO enforces the rules
// independently so the platform works even when SeaweedFS' native
// lifecycle support is incomplete (WS-29 "Notes" caveat).
package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// LifecycleAction is the service-layer enum for a lifecycle rule's
// action. Mirrors providers/seaweedfs.LifecycleAction and the
// storage_lifecycle_rules.action CHECK constraint.
type LifecycleAction string

const (
	LifecycleActionExpiration                  LifecycleAction = "expiration"
	LifecycleActionNoncurrentVersionExpiration LifecycleAction = "noncurrent_version_expiration"
	LifecycleActionAbortIncompleteMultipart    LifecycleAction = "abort_incomplete_multipart"
	LifecycleActionTransition                  LifecycleAction = "transition"
)

// toDriverLifecycleAction maps the service enum to the driver enum.
func toDriverLifecycleAction(a LifecycleAction) seaweedfs.LifecycleAction {
	switch a {
	case LifecycleActionExpiration:
		return seaweedfs.LifecycleActionExpiration
	case LifecycleActionNoncurrentVersionExpiration:
		return seaweedfs.LifecycleActionNoncurrentVersionExpiration
	case LifecycleActionAbortIncompleteMultipart:
		return seaweedfs.LifecycleActionAbortIncompleteMultipart
	case LifecycleActionTransition:
		return seaweedfs.LifecycleActionTransition
	}
	return seaweedfs.LifecycleActionExpiration
}

// fromRowLifecycleAction maps the TEXT column value to the service enum.
func fromRowLifecycleAction(s string) LifecycleAction {
	switch s {
	case "expiration":
		return LifecycleActionExpiration
	case "noncurrent_version_expiration":
		return LifecycleActionNoncurrentVersionExpiration
	case "abort_incomplete_multipart":
		return LifecycleActionAbortIncompleteMultipart
	case "transition":
		return LifecycleActionTransition
	}
	return LifecycleActionExpiration
}

// LifecycleRule is the service-layer value type for a single rule.
type LifecycleRule struct {
	// ID is the user-provided rule identifier within the bucket.
	ID string

	// Enabled is true when the rule should be considered by the evaluator.
	Enabled bool

	// Action is the lifecycle action.
	Action LifecycleAction

	// Days is the trigger age in days. Mutually exclusive with Date.
	Days *int32

	// Date is the trigger date. Mutually exclusive with Days.
	Date *time.Time

	// StorageClass is required for transition rules; ignored for the others.
	StorageClass string

	// Prefix is the key-prefix filter; empty = whole bucket.
	Prefix string
}

// LifecycleRuleInput is the user-supplied input on Create / Update.
// Mirrors LifecycleRule but with looser pointer semantics so the JSON
// request body shapes the OpenAPI schemas cleanly.
type LifecycleRuleInput struct {
	ID           string
	Enabled      bool
	Action       LifecycleAction
	Days         *int32
	Date         *time.Time
	StorageClass *string
	Prefix       *string
}

// validateLifecycleInput enforces the invariants the database-level
// CHECK constraints ALSO enforce, but at the service boundary so a bad
// request surfaces as a 400 with a useful message rather than a 500
// from a database error.
func validateLifecycleInput(r LifecycleRuleInput) error {
	if strings.TrimSpace(r.ID) == "" {
		return ErrInvalidLifecycleRuleID
	}
	if len(r.ID) > 255 {
		return ErrInvalidLifecycleRuleID
	}
	switch r.Action {
	case LifecycleActionExpiration,
		LifecycleActionNoncurrentVersionExpiration,
		LifecycleActionAbortIncompleteMultipart,
		LifecycleActionTransition:
		// ok
	default:
		return ErrInvalidLifecycleAction
	}
	// Exactly one of Days or Date must be set.
	if (r.Days == nil) == (r.Date == nil) {
		return ErrInvalidLifecycleTrigger
	}
	if r.Days != nil && *r.Days <= 0 {
		return ErrInvalidLifecycleTrigger
	}
	if r.Action == LifecycleActionTransition {
		if r.StorageClass == nil || strings.TrimSpace(*r.StorageClass) == "" {
			return ErrInvalidLifecycleStorageClass
		}
	} else if r.StorageClass != nil && strings.TrimSpace(*r.StorageClass) != "" {
		return ErrInvalidLifecycleStorageClass
	}
	return nil
}

// CreateLifecycleRule creates a single rule. The caller has already
// passed rbac.PermS3BucketLifecycle at the HTTP boundary.
func (s *Service) CreateLifecycleRule(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	in LifecycleRuleInput,
) (database.StorageLifecycleRule, error) {
	if s.provider == nil {
		return database.StorageLifecycleRule{}, ErrProviderDisabled
	}
	if err := validateLifecycleInput(in); err != nil {
		return database.StorageLifecycleRule{}, err
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.StorageLifecycleRule{}, ErrBucketNotFound
		}
		return database.StorageLifecycleRule{}, fmt.Errorf("storage.lifecycle.create: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketLifecycleSet,
		ResourceType: ResourceLifecycleRule,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"rule_id":     in.ID,
			"action":      string(in.Action),
		},
	})

	status := "disabled"
	if in.Enabled {
		status = "enabled"
	}
	ruleRow, err := s.repos.StorageLifecycleRules.Create(ctx, database.CreateStorageLifecycleRuleParams{
		BucketID:     bucketID,
		RuleID:       in.ID,
		Status:       status,
		Action:       string(in.Action),
		Days:         in.Days,
		DateAt:       in.Date,
		StorageClass: in.StorageClass,
		Prefix:       in.Prefix,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.StorageLifecycleRule{}, fmt.Errorf("storage.lifecycle.create: %w", err)
	}

	// Push the updated rule set to SeaweedFS. Best-effort: a failed push
	// does not roll back the row; the evaluator worker will pick up the
	// rule regardless. The audit row carries the push result so the
	// operator can see when SeaweedFS' native lifecycle support is
	// absent.
	if err := s.reconcileLifecycleToProvider(ctx, row.Name, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
			"warning":              "rule stored; backend push failed (lifecycle worker will enforce)",
			"backend_push_error":   err.Error(),
		}})
	} else {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	}

	s.emitEvent(ctx, eventbus.S3BucketLifecycleSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"rule_id":     ruleRow.RuleID,
		"action":      "create",
	})
	return ruleRow, nil
}

// GetLifecycleRule returns the rule with the given natural key.
func (s *Service) GetLifecycleRule(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
	ruleID string,
) (database.StorageLifecycleRule, error) {
	row, err := s.repos.StorageLifecycleRules.Get(ctx, bucketID, ruleID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.StorageLifecycleRule{}, ErrLifecycleRuleNotFound
		}
		return database.StorageLifecycleRule{}, fmt.Errorf("storage.lifecycle.get: %w", err)
	}
	return row, nil
}

// ListLifecycleRules returns the rules attached to the bucket within
// the tenant in ctx.
func (s *Service) ListLifecycleRules(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
	limit, offset int32,
) ([]database.StorageLifecycleRule, error) {
	rows, err := s.repos.StorageLifecycleRules.List(ctx, bucketID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("storage.lifecycle.list: %w", err)
	}
	return rows, nil
}

// CountLifecycleRules returns the count of rules on the bucket.
func (s *Service) CountLifecycleRules(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (int64, error) {
	return s.repos.StorageLifecycleRules.Count(ctx, bucketID)
}

// UpdateLifecycleRule replaces the mutable fields of a rule. The rule's
// natural key (rule_id) and bucket_id are immutable; a rule cannot hop
// buckets.
func (s *Service) UpdateLifecycleRule(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID, ruleRowID uuid.UUID,
	in LifecycleRuleInput,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	existing, err := s.repos.StorageLifecycleRules.GetByID(ctx, ruleRowID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrLifecycleRuleNotFound
		}
		return fmt.Errorf("storage.lifecycle.update: %w", err)
	}
	if existing.BucketID != bucketID {
		// Cross-bucket hop: reject as not-found so the caller cannot
		// probe for rule ids in other buckets.
		return ErrLifecycleRuleNotFound
	}
	// The validation uses the existing ID + action because those are
	// immutable; only the trigger fields + status + storage_class +
	// prefix are mutable.
	toValidate := LifecycleRuleInput{
		ID:           existing.RuleID,
		Enabled:      in.Enabled,
		Action:       fromRowLifecycleAction(existing.Action),
		Days:         in.Days,
		Date:         in.Date,
		StorageClass: in.StorageClass,
		Prefix:       in.Prefix,
	}
	if err := validateLifecycleInput(toValidate); err != nil {
		return err
	}

	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.lifecycle.update: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketLifecycleSet,
		ResourceType: ResourceLifecycleRule,
		ResourceID:   &existing.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"rule_id":     existing.RuleID,
			"action":      "update",
		},
	})

	status := "disabled"
	if in.Enabled {
		status = "enabled"
	}
	if err := s.repos.StorageLifecycleRules.Update(ctx, database.UpdateStorageLifecycleRuleParams{
		ID:           ruleRowID,
		Status:       status,
		Days:         in.Days,
		DateAt:       in.Date,
		StorageClass: in.StorageClass,
		Prefix:       in.Prefix,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.lifecycle.update: %w", err)
	}

	if err := s.reconcileLifecycleToProvider(ctx, row.Name, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
			"warning":            "rule updated; backend push failed (lifecycle worker will enforce)",
			"backend_push_error": err.Error(),
		}})
	} else {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	}

	s.emitEvent(ctx, eventbus.S3BucketLifecycleSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"rule_id":     existing.RuleID,
		"action":      "update",
	})
	return nil
}

// SetLifecycleRuleStatus is a convenience shortcut for the enable /
// disable toggle without rewriting the rest of the rule.
func (s *Service) SetLifecycleRuleStatus(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID, ruleRowID uuid.UUID,
	enabled bool,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	existing, err := s.repos.StorageLifecycleRules.GetByID(ctx, ruleRowID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrLifecycleRuleNotFound
		}
		return fmt.Errorf("storage.lifecycle.status: %w", err)
	}
	if existing.BucketID != bucketID {
		return ErrLifecycleRuleNotFound
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.lifecycle.status: %w", err)
	}

	status := "disabled"
	if enabled {
		status = "enabled"
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketLifecycleSet,
		ResourceType: ResourceLifecycleRule,
		ResourceID:   &existing.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"rule_id":     existing.RuleID,
			"action":      "set_status",
			"new_status":  status,
		},
	})
	if err := s.repos.StorageLifecycleRules.SetStatus(ctx, ruleRowID, status); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.lifecycle.status: %w", err)
	}
	if err := s.reconcileLifecycleToProvider(ctx, row.Name, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
			"warning":            "rule updated; backend push failed (lifecycle worker will enforce)",
			"backend_push_error": err.Error(),
		}})
	} else {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	}
	s.emitEvent(ctx, eventbus.S3BucketLifecycleSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"rule_id":     existing.RuleID,
		"action":      "set_status",
	})
	return nil
}

// DeleteLifecycleRule removes the rule. Idempotent at the HTTP layer;
// returns ErrLifecycleRuleNotFound when the rule is missing so the
// handler can decide whether to surface 404 or 204.
func (s *Service) DeleteLifecycleRule(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID, ruleRowID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	existing, err := s.repos.StorageLifecycleRules.GetByID(ctx, ruleRowID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrLifecycleRuleNotFound
		}
		return fmt.Errorf("storage.lifecycle.delete: %w", err)
	}
	if existing.BucketID != bucketID {
		return ErrLifecycleRuleNotFound
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.lifecycle.delete: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketLifecycleSet,
		ResourceType: ResourceLifecycleRule,
		ResourceID:   &existing.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"rule_id":     existing.RuleID,
			"action":      "delete",
		},
	})
	if err := s.repos.StorageLifecycleRules.Delete(ctx, ruleRowID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.lifecycle.delete: %w", err)
	}
	if err := s.reconcileLifecycleToProvider(ctx, row.Name, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
			"warning":            "rule deleted; backend push failed (lifecycle worker will catch up)",
			"backend_push_error": err.Error(),
		}})
	} else {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	}
	s.emitEvent(ctx, eventbus.S3BucketLifecycleSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"rule_id":     existing.RuleID,
		"action":      "delete",
	})
	return nil
}

// ReplaceLifecycleRules replaces the entire rule set for the bucket.
// The previous rules are deleted in bulk and the new rules are written
// in the same call (the caller is responsible for transaction
// semantics; the underlying queries are independent statements so a
// mid-call failure leaves a partial state — the audit row records the
// outcome). The push to SeaweedFS happens once at the end so the
// daemon sees the full new policy atomically.
func (s *Service) ReplaceLifecycleRules(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	rules []LifecycleRuleInput,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	for _, r := range rules {
		if err := validateLifecycleInput(r); err != nil {
			return err
		}
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.lifecycle.replace: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketLifecycleSet,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"rule_count":  len(rules),
			"action":      "replace_all",
		},
	})

	if err := s.repos.StorageLifecycleRules.DeleteAllForBucket(ctx, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.lifecycle.replace: %w", err)
	}
	for _, r := range rules {
		status := "disabled"
		if r.Enabled {
			status = "enabled"
		}
		if _, err := s.repos.StorageLifecycleRules.Create(ctx, database.CreateStorageLifecycleRuleParams{
			BucketID:     bucketID,
			RuleID:       r.ID,
			Status:       status,
			Action:       string(r.Action),
			Days:         r.Days,
			DateAt:       r.Date,
			StorageClass: r.StorageClass,
			Prefix:       r.Prefix,
		}); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error":   err.Error(),
				"rule_id": r.ID,
			}})
			return fmt.Errorf("storage.lifecycle.replace: %w", err)
		}
	}

	if err := s.reconcileLifecycleToProvider(ctx, row.Name, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
			"warning":            "rules stored; backend push failed (lifecycle worker will enforce)",
			"backend_push_error": err.Error(),
		}})
	} else {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	}

	s.emitEvent(ctx, eventbus.S3BucketLifecycleSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"rule_count":  len(rules),
		"action":      "replace_all",
	})
	return nil
}

// reconcileLifecycleToProvider pushes the current rule set for the
// bucket to SeaweedFS so the daemon can honour the policy natively.
// Used by every Create / Update / Delete / Replace / Set-status path.
//
// A failure here is non-fatal: the lifecycle evaluator worker will
// enforce the rules independently on its next tick. The audit row
// carries the push result so the operator can see when SeaweedFS'
// native lifecycle support is absent or partial.
func (s *Service) reconcileLifecycleToProvider(ctx context.Context, bucketName string, bucketID uuid.UUID) error {
	rules, err := s.repos.StorageLifecycleRules.List(ctx, bucketID, 1000, 0)
	if err != nil {
		return fmt.Errorf("list rules for push: %w", err)
	}
	driverRules := make([]seaweedfs.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		driverRules = append(driverRules, ruleRowToDriver(r))
	}
	if err := s.provider.SetBucketLifecycle(ctx, bucketName, driverRules); err != nil {
		if errors.Is(err, seaweedfs.ErrNotFound) {
			return nil // bucket was deleted; nothing to push.
		}
		return err
	}
	return nil
}

// ruleRowToDriver translates a storage_lifecycle_rules row to the
// driver value type. StorageClass + Prefix pointers become values
// (empty string when nil) because the driver accepts both shapes.
func ruleRowToDriver(r database.StorageLifecycleRule) seaweedfs.LifecycleRule {
	out := seaweedfs.LifecycleRule{
		ID:      r.RuleID,
		Enabled: r.Status == "enabled",
		Action:  seaweedfs.LifecycleAction(r.Action),
	}
	if r.Days != nil {
		out.Days = *r.Days
	}
	if r.DateAt != nil {
		out.Date = r.DateAt.Format("2006-01-02")
	}
	if r.StorageClass != nil {
		out.StorageClass = *r.StorageClass
	}
	if r.Prefix != nil {
		out.Prefix = *r.Prefix
	}
	return out
}
