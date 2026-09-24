// Package api: storage_handlers.go implements the OpenAPI-derived object
// storage endpoints (WS-16). Each handler is thin: parse request, resolve
// IDs from the request scope, call the storage service, render the
// response. Every privileged route is gated by RequirePerm via the audit
// gate middleware (extended in router.go to cover /api/v1/storage/*).
//
// Error mapping follows the WS-16 DoD:
//
//   - 400 bad_request          -> malformed body / params / invalid content
//   - 401 unauthorized         -> no session (auth middleware)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 404 not_found            -> bucket / credential missing
//   - 409 conflict             -> canonical bucket name taken
//   - 422 unprocessable_entity -> per-tenant quota cap exceeded (reserved)
//   - 501 not_implemented      -> SeaweedFS provider disabled
//
// Per ADR-0011 the data plane is direct from the user's S3 client to
// SeaweedFS; Lahijan only manages the control plane. The mint-credential
// endpoint returns the plaintext secret exactly ONCE; subsequent reads
// (list / get) return only the access key.
package api

import (
	"errors"
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultStoragePageSize is the page size for storage list endpoints when
// the caller does not pass one. Mirrors DefaultDNSPageSize /
// DefaultComputePageSize so the three areas render consistently.
const DefaultStoragePageSize = 50

// MaxStoragePageSize caps a single page so a misbehaving client cannot
// request millions of rows in one call.
const MaxStoragePageSize = 200

// storagePageParams clamps + defaults limit/offset for the storage list
// endpoints.
func storagePageParams(limit, offset *int) (int32, int32) {
	lim := DefaultStoragePageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxStoragePageSize {
		lim = MaxStoragePageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return int32(lim), int32(off)
}

// storageDisabledMsg is the localised "feature disabled" message the
// handlers return when the SeaweedFS provider is not wired.
func storageDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "s3.err_disabled", nil)
}

// storageTenantAndUser resolves the tenant + user id pair from the
// request scope. Mirrors computeTenantAndUser / dnsTenantAndUser.
func (s *Server) storageTenantAndUser(c *fiber.Ctx) (uuid.UUID, uuid.UUID, bool) {
	uid, ok := currentUserID(c)
	if !ok {
		_ = SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		return uuid.Nil, uuid.Nil, false
	}
	tid, err := database.TenantFromContext(c.UserContext())
	if err != nil {
		_ = SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		return uuid.Nil, uuid.Nil, false
	}
	return tid, uid, true
}

// mapStorageError translates a storage service error to the right
// envelope.
func mapStorageError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, storage.ErrProviderDisabled):
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	case errors.Is(err, storage.ErrBucketNotFound), errors.Is(err, storage.ErrCredentialNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "s3.err_not_found", nil))
	case errors.Is(err, storage.ErrBucketAlreadyExists):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "s3.err_already_exists", nil), nil)
	case errors.Is(err, storage.ErrInvalidBucketSlug),
		errors.Is(err, storage.ErrInvalidQuota),
		errors.Is(err, storage.ErrInvalidAction),
		errors.Is(err, storage.ErrInvalidPresignMethod),
		errors.Is(err, storage.ErrInvalidPresignTTL),
		errors.Is(err, storage.ErrInvalidLabel):
		// Surface the validator's message verbatim so the UI can render it.
		return SendBadRequest(c, err.Error(), nil)
	}
	logUnexpectedError(c, "storage", err)
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// ---------------------------------------------------------------------------
// Buckets
// ---------------------------------------------------------------------------

// ListStorageBuckets handles GET /api/v1/storage/buckets.
func (s *Server) ListStorageBuckets(c *fiber.Ctx, params apigen.ListStorageBucketsParams) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := storagePageParams(params.Limit, params.Offset)
	rows, err := s.storageSvc.ListBuckets(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapStorageError(c, err)
	}
	items := make([]apigen.StorageBucket, 0, len(rows))
	for _, row := range rows {
		items = append(items, toStorageBucketDTO(row))
	}
	total, err := s.storageSvc.CountBuckets(c.UserContext(), tid)
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.JSON(apigen.StorageBucketPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateStorageBucket handles POST /api/v1/storage/buckets.
func (s *Server) CreateStorageBucket(c *fiber.Ctx) error {
	var req apigen.StorageBucketCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Slug == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "s3.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	quotaBytes := int64(0)
	if req.QuotaBytes != nil {
		quotaBytes = *req.QuotaBytes
	}
	quotaObjects := int64(0)
	if req.QuotaObjects != nil {
		quotaObjects = *req.QuotaObjects
	}
	row, err := s.storageSvc.CreateBucket(c.UserContext(), tid, uid, storage.BucketCreateParams{
		Slug:         req.Slug,
		Label:        ptrString(req.Label),
		Description:  ptrString(req.Description),
		QuotaBytes:   quotaBytes,
		QuotaObjects: quotaObjects,
	})
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toStorageBucketDTO(row))
}

// GetStorageBucket handles GET /api/v1/storage/buckets/{bucketId}.
func (s *Server) GetStorageBucket(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.storageSvc.GetBucket(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.JSON(toStorageBucketDTO(row))
}

// UpdateStorageBucket handles PATCH /api/v1/storage/buckets/{bucketId}.
func (s *Server) UpdateStorageBucket(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageBucketUpdateRequest
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
	row, err := s.storageSvc.UpdateBucket(c.UserContext(), tid, uid, bucketID, storage.BucketUpdateParams{
		Label:       req.Label,
		Description: req.Description,
	})
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.JSON(toStorageBucketDTO(row))
}

// DeleteStorageBucket handles DELETE /api/v1/storage/buckets/{bucketId}.
func (s *Server) DeleteStorageBucket(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.storageSvc.DeleteBucket(c.UserContext(), tid, uid, bucketID); err != nil {
		return mapStorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

// ListStorageCredentials handles GET /api/v1/storage/buckets/{bucketId}/credentials.
func (s *Server) ListStorageCredentials(
	c *fiber.Ctx,
	bucketID openapi_types.UUID,
	params apigen.ListStorageCredentialsParams,
) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := storagePageParams(params.Limit, params.Offset)
	rows, err := s.storageSvc.ListCredentials(c.UserContext(), tid, bucketID, limit, offset)
	if err != nil {
		return mapStorageError(c, err)
	}
	items := make([]apigen.StorageCredential, 0, len(rows))
	for _, row := range rows {
		items = append(items, toStorageCredentialDTO(row))
	}
	total, err := s.storageSvc.CountCredentials(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.JSON(apigen.StorageCredentialPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateStorageCredential handles POST /api/v1/storage/buckets/{bucketId}/credentials.
//
// The plaintext secret is returned in the response body EXACTLY ONCE;
// the dashboard must surface it inline + offer a copy button. Lahijan
// cannot recover the plaintext after this response is sent.
func (s *Server) CreateStorageCredential(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageCredentialCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if len(req.Actions) == 0 {
		return SendBadRequest(c, i18n.T(c.UserContext(), "s3.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	// Translate the wire actions to the service-layer enum.
	actions := make([]storage.CredentialAction, 0, len(req.Actions))
	for _, a := range req.Actions {
		actions = append(actions, storage.CredentialAction(a))
	}
	// Optional expiry. expiresInSeconds == nil means "no expiry".
	var expiresAt *time.Time
	if req.ExpiresInSeconds != nil && *req.ExpiresInSeconds > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresInSeconds) * time.Second)
		expiresAt = &t
	}
	minted, err := s.storageSvc.MintCredential(c.UserContext(), tid, uid, bucketID, storage.CredentialMintParams{
		Label:     ptrString(req.Label),
		Actions:   actions,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(apigen.StorageCredentialWithSecret{
		Credential: toStorageCredentialDTO(minted.Row),
		SecretKey:  minted.SecretKey,
	})
}

// RevokeStorageCredential handles DELETE /api/v1/storage/buckets/{bucketId}/credentials/{credentialId}.
func (s *Server) RevokeStorageCredential(
	c *fiber.Ctx,
	bucketID, credentialID openapi_types.UUID,
) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.storageSvc.RevokeCredential(c.UserContext(), tid, uid, bucketID, credentialID); err != nil {
		return mapStorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Presign
// ---------------------------------------------------------------------------

// PresignStorageObject handles POST /api/v1/storage/buckets/{bucketId}/presign.
func (s *Server) PresignStorageObject(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StoragePresignRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Method == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "s3.err_bad_request", nil), nil)
	}
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, uid, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	ttl := time.Duration(0)
	if req.ExpiresInSeconds != nil && *req.ExpiresInSeconds > 0 {
		ttl = time.Duration(*req.ExpiresInSeconds) * time.Second
	}
	res, err := s.storageSvc.Presign(c.UserContext(), tid, uid, bucketID, storage.PresignParams{
		Method:    storage.PresignMethod(req.Method),
		Key:       ptrString(req.Key),
		ExpiresIn: ttl,
	})
	if err != nil {
		return mapStorageError(c, err)
	}
	out := apigen.StoragePresignResult{
		Url:       res.URL,
		Method:    apigen.StoragePresignResultMethod(res.Method),
		Bucket:    res.Bucket,
		ExpiresAt: res.ExpiresAt,
	}
	if res.Key != "" {
		k := res.Key
		out.Key = &k
	}
	return c.JSON(out)
}

// ---------------------------------------------------------------------------
// Quota + Usage
// ---------------------------------------------------------------------------

// SetStorageBucketQuota handles POST /api/v1/storage/buckets/{bucketId}/quota.
func (s *Server) SetStorageBucketQuota(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	var req apigen.StorageQuotaRequest
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
	if err := s.storageSvc.SetBucketQuota(c.UserContext(), tid, uid, bucketID, storage.QuotaSpec{
		QuotaBytes:   req.QuotaBytes,
		QuotaObjects: req.QuotaObjects,
	}); err != nil {
		return mapStorageError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetStorageBucketUsage handles GET /api/v1/storage/buckets/{bucketId}/usage.
func (s *Server) GetStorageBucketUsage(c *fiber.Ctx, bucketID openapi_types.UUID) error {
	if s.storageSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, storageDisabledMsg(c), nil)
	}
	tid, _, ok := s.storageTenantAndUser(c)
	if !ok {
		return nil
	}
	usage, err := s.storageSvc.GetBucketUsage(c.UserContext(), tid, bucketID)
	if err != nil {
		return mapStorageError(c, err)
	}
	return c.JSON(apigen.StorageBucketUsage{
		QuotaBytes:   usage.QuotaBytes,
		QuotaObjects: usage.QuotaObjects,
		BytesUsed:    usage.BytesUsed,
		ObjectsUsed:  usage.ObjectsUsed,
	})
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

// toStorageBucketDTO converts a database.StorageBucket row to the OpenAPI
// StorageBucket schema.
func toStorageBucketDTO(row database.StorageBucket) apigen.StorageBucket {
	out := apigen.StorageBucket{
		Id:           row.ID,
		TenantId:     row.TenantID,
		Name:         row.Name,
		Slug:         row.Slug,
		OwnerId:      row.OwnerUserID,
		QuotaBytes:   row.QuotaBytes,
		QuotaObjects: row.QuotaObjects,
		BytesUsed:    row.BytesUsed,
		ObjectsUsed:  row.ObjectsUsed,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    &row.UpdatedAt,
	}
	if row.Label != "" {
		out.Label = &row.Label
	}
	if row.Description != "" {
		out.Description = &row.Description
	}
	return out
}

// toStorageCredentialDTO converts a database.StorageCredential row to the
// OpenAPI StorageCredential schema. The plaintext secret is NEVER
// rendered here; it is returned via StorageCredentialWithSecret exactly
// once at mint time.
func toStorageCredentialDTO(row database.StorageCredential) apigen.StorageCredential {
	out := apigen.StorageCredential{
		Id:          row.ID,
		BucketId:    row.BucketID,
		TenantId:    row.TenantID,
		UserId:      row.UserID,
		AccessKeyId: row.AccessKeyID,
		CreatedAt:   row.CreatedAt,
	}
	if row.Label != "" {
		out.Label = &row.Label
	}
	if len(row.Actions) > 0 {
		actions := make([]apigen.StorageCredentialActions, 0, len(row.Actions))
		for _, a := range row.Actions {
			actions = append(actions, apigen.StorageCredentialActions(a))
		}
		out.Actions = actions
	}
	if row.LastUsedAt != nil {
		out.LastUsedAt = row.LastUsedAt
	}
	if row.ExpiresAt != nil {
		out.ExpiresAt = row.ExpiresAt
	}
	if row.RevokedAt != nil {
		out.RevokedAt = row.RevokedAt
	}
	return out
}
