// Package api: storage_ws29_handlers.go implements the OpenAPI-derived
// WS-29 endpoints (versioning + lifecycle rules + object-lock + version
// listing). Each handler is thin: parse request, resolve IDs from the
// request scope, call the storage service, render the response. Every
// privileged route is gated by RequirePerm via the audit gate
// middleware (extended in router.go to cover the new sub-tree paths).
//
// Error mapping follows the WS-16 storage_handlers.go pattern:
//
//   - 400 bad_request          -> malformed body / invalid input
//   - 401 unauthorized         -> no session (auth middleware)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 404 not_found            -> bucket / rule / version missing
//   - 409 conflict             -> (reserved for future use)
//   - 501 not_implemented      -> SeaweedFS provider disabled
package api

import (
	"errors"
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/gofiber/fiber/v2"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// mapWS29StorageError extends mapStorageError with the WS-29 sentinels.
// Errors not handled here fall through to mapStorageError.
func mapWS29StorageError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, storage.ErrLifecycleRuleNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "s3.err_not_found", nil))
	case errors.Is(err, storage.ErrInvalidLifecycleRuleID),
		errors.Is(err, storage.ErrInvalidLifecycleAction),
		errors.Is(err, storage.ErrInvalidLifecycleTrigger),
		errors.Is(err, storage.ErrInvalidLifecycleStorageClass),
		errors.Is(err, storage.ErrInvalidObjectLockMode),
		errors.Is(err, storage.ErrInvalidObjectLockDays):
		return SendBadRequest(c, err.Error(), nil)
	}
	return mapStorageError(c, err)
}

// ---------------------------------------------------------------------------
// Versioning
// ---------------------------------------------------------------------------

// GetStorageBucketVersioning handles GET /api/v1/storage/buckets/{bucketId}/versioning.
func (s *Server) GetStorageBucketVersioning(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	status, err := s.storageSvc.GetBucketVersioning(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.JSON(apigen.StorageVersioningStatus{
		Status: apigen.StorageVersioningStatusStatus(status),
	})
}

// SetStorageBucketVersioning handles PUT /api/v1/storage/buckets/{bucketId}/versioning.
func (s *Server) SetStorageBucketVersioning(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageVersioningStatus
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.storageSvc.SetBucketVersioning(c.UserContext(), tid, uid, bucketID, storage.VersioningStatus(req.Status)); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListStorageBucketVersions handles GET /api/v1/storage/buckets/{bucketId}/versions.
func (s *Server) ListStorageBucketVersions(c *fiber.Ctx, bucketID openapi_types.UUID, params apigen.ListStorageBucketVersionsParams) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	prefix := ptrString(params.Prefix)
	keyMarker := ptrString(params.KeyMarker)
	versionMarker := ptrString(params.VersionIdMarker)
	maxKeys := int32(1000)
	if params.MaxKeys != nil {
		maxKeys = int32(*params.MaxKeys)
	}
	page, err := s.storageSvc.ListObjectVersions(c.UserContext(), tid, bucketID, prefix, keyMarker, versionMarker, maxKeys)
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	out := apigen.StorageObjectVersionsPage{
		Versions:      make([]apigen.StorageObjectVersion, 0, len(page.Versions)),
		DeleteMarkers: make([]apigen.StorageObjectVersion, 0, len(page.DeleteMarkers)),
		IsTruncated:   page.IsTruncated,
	}
	if page.NextKeyMarker != "" {
		out.NextKeyMarker = &page.NextKeyMarker
	}
	if page.NextVersionIDMarker != "" {
		out.NextVersionIdMarker = &page.NextVersionIDMarker
	}
	for _, v := range page.Versions {
		out.Versions = append(out.Versions, toStorageObjectVersionDTO(v))
	}
	for _, d := range page.DeleteMarkers {
		out.DeleteMarkers = append(out.DeleteMarkers, toStorageObjectVersionDTO(d))
	}
	return c.JSON(out)
}

// RestoreStorageObjectVersion handles POST /api/v1/storage/buckets/{bucketId}/versions/restore.
func (s *Server) RestoreStorageObjectVersion(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageRestoreVersionRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Key == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "s3.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	vid := ""
	if req.VersionId != nil {
		vid = *req.VersionId
	}
	if err := s.storageSvc.RestoreObjectVersion(c.UserContext(), tid, uid, bucketID, req.Key, vid); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Lifecycle rules
// ---------------------------------------------------------------------------

// ListStorageBucketLifecycleRules handles GET /api/v1/storage/buckets/{bucketId}/lifecycle.
//
//nolint:lll // signature mirrors the generated ServerInterface; cannot wrap.
func (s *Server) ListStorageBucketLifecycleRules(c *fiber.Ctx, bucketID openapi_types.UUID, params apigen.ListStorageBucketLifecycleRulesParams) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := storagePageParams(params.Limit, params.Offset)
	rows, err := s.storageSvc.ListLifecycleRules(c.UserContext(), tid, bucketID, limit, offset)
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	items := make([]apigen.StorageLifecycleRule, 0, len(rows))
	for _, r := range rows {
		items = append(items, toStorageLifecycleRuleDTO(r))
	}
	total, err := s.storageSvc.CountLifecycleRules(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.JSON(apigen.StorageLifecycleRulePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateStorageLifecycleRule handles POST /api/v1/storage/buckets/{bucketId}/lifecycle.
func (s *Server) CreateStorageLifecycleRule(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageLifecycleRuleCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.RuleId == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "s3.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.storageSvc.CreateLifecycleRule(c.UserContext(), tid, uid, bucketID, ws29LifecycleCreateInput(req))
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toStorageLifecycleRuleDTO(row))
}

// ReplaceStorageBucketLifecycleRules handles PUT /api/v1/storage/buckets/{bucketId}/lifecycle.
func (s *Server) ReplaceStorageBucketLifecycleRules(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageLifecycleRuleReplaceRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	rules := make([]storage.LifecycleRuleInput, 0, len(req.Rules))
	for _, r := range req.Rules {
		rules = append(rules, ws29LifecycleCreateInput(r))
	}
	if err := s.storageSvc.ReplaceLifecycleRules(c.UserContext(), tid, uid, bucketID, rules); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// UpdateStorageLifecycleRule handles PATCH /api/v1/storage/buckets/{bucketId}/lifecycle/{ruleId}.
func (s *Server) UpdateStorageLifecycleRule(c *fiber.Ctx, bucketID, ruleID openapi_types.UUID) error {
	var req apigen.StorageLifecycleRuleUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := s.storageSvc.UpdateLifecycleRule(c.UserContext(), tid, uid, bucketID, ruleID, storage.LifecycleRuleInput{
		Enabled:      enabled,
		Days:         req.Days,
		Date:         ws29DatePtr(req.Date),
		StorageClass: req.StorageClass,
		Prefix:       req.Prefix,
	}); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteStorageLifecycleRule handles DELETE /api/v1/storage/buckets/{bucketId}/lifecycle/{ruleId}.
func (s *Server) DeleteStorageLifecycleRule(c *fiber.Ctx, bucketID, ruleID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.storageSvc.DeleteLifecycleRule(c.UserContext(), tid, uid, bucketID, ruleID); err != nil {
		// HTTP DELETE is idempotent; a missing rule surfaces as 204
		// rather than 404 so a duplicate client call does not error.
		if errors.Is(err, storage.ErrLifecycleRuleNotFound) {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SetStorageLifecycleRuleStatus handles POST /api/v1/storage/buckets/{bucketId}/lifecycle/{ruleId}/status.
func (s *Server) SetStorageLifecycleRuleStatus(c *fiber.Ctx, bucketID, ruleID openapi_types.UUID) error {
	var req apigen.StorageLifecycleRuleStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.storageSvc.SetLifecycleRuleStatus(c.UserContext(), tid, uid, bucketID, ruleID, req.Enabled); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Object lock
// ---------------------------------------------------------------------------

// GetStorageBucketObjectLock handles GET /api/v1/storage/buckets/{bucketId}/object-lock.
func (s *Server) GetStorageBucketObjectLock(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	cfg, err := s.storageSvc.GetObjectLock(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.JSON(toStorageObjectLockDTO(cfg))
}

// SetStorageBucketObjectLock handles PUT /api/v1/storage/buckets/{bucketId}/object-lock.
func (s *Server) SetStorageBucketObjectLock(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageObjectLockConfig
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	mode := ""
	if req.Mode != nil {
		mode = string(*req.Mode)
	}
	days := int32(0)
	if req.Days != nil {
		days = *req.Days
	}
	if err := s.storageSvc.SetObjectLock(c.UserContext(), tid, uid, bucketID, storage.ObjectLockConfig{
		Enabled: req.Enabled,
		Mode:    storage.ObjectLockMode(mode),
		Days:    days,
	}); err != nil {
		return mapWS29StorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

// toStorageLifecycleRuleDTO converts a storage_lifecycle_rules row to
// the OpenAPI StorageLifecycleRule schema.
func toStorageLifecycleRuleDTO(row database.StorageLifecycleRule) apigen.StorageLifecycleRule {
	out := apigen.StorageLifecycleRule{
		Id:       row.ID,
		BucketId: row.BucketID,
		RuleId:   row.RuleID,
		Status:   apigen.StorageLifecycleRuleStatus(row.Status),
		Enabled:  row.Status == "enabled",
		Action:   apigen.StorageLifecycleRuleAction(row.Action),
	}
	if row.Days != nil {
		d := *row.Days
		out.Days = &d
	}
	if row.DateAt != nil {
		d := openapi_types.Date{Time: *row.DateAt}
		out.Date = &d
	}
	if row.StorageClass != nil {
		out.StorageClass = row.StorageClass
	}
	if row.Prefix != nil {
		out.Prefix = row.Prefix
	}
	created := row.CreatedAt
	out.CreatedAt = &created
	updated := row.UpdatedAt
	out.UpdatedAt = &updated
	return out
}

// toStorageObjectVersionDTO converts a driver-level ObjectVersion to
// the OpenAPI StorageObjectVersion schema.
func toStorageObjectVersionDTO(v seaweedfs.ObjectVersion) apigen.StorageObjectVersion {
	out := apigen.StorageObjectVersion{
		Key:       v.Key,
		VersionId: v.VersionID,
		IsLatest:  v.IsLatest,
	}
	if v.IsDeleteMarker {
		out.IsDeleteMarker = &v.IsDeleteMarker
	}
	if v.Size > 0 {
		size := v.Size
		out.Size = &size
	}
	if !v.LastModified.IsZero() {
		lm := v.LastModified
		out.LastModified = &lm
	}
	return out
}

// toStorageObjectLockDTO converts a service-layer ObjectLockConfig to
// the OpenAPI StorageObjectLockConfig schema.
func toStorageObjectLockDTO(cfg storage.ObjectLockConfig) apigen.StorageObjectLockConfig {
	out := apigen.StorageObjectLockConfig{Enabled: cfg.Enabled}
	if cfg.Mode != "" {
		m := apigen.StorageObjectLockConfigMode(cfg.Mode)
		out.Mode = &m
	}
	if cfg.Days > 0 {
		d := cfg.Days
		out.Days = &d
	}
	return out
}

// ws29LifecycleCreateInput translates the wire request shape to the
// service-layer input shape.
func ws29LifecycleCreateInput(req apigen.StorageLifecycleRuleCreateRequest) storage.LifecycleRuleInput {
	out := storage.LifecycleRuleInput{
		ID:     req.RuleId,
		Action: storage.LifecycleAction(req.Action),
	}
	if req.Enabled != nil {
		out.Enabled = *req.Enabled
	} else {
		out.Enabled = true
	}
	out.Days = req.Days
	out.Date = ws29DatePtr(req.Date)
	out.StorageClass = req.StorageClass
	out.Prefix = req.Prefix
	return out
}

// ws29DatePtr converts an openapi_types.Date to a *time.Time. Returns
// nil when the input is nil.
func ws29DatePtr(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}
