// Package database: compute_backup_targets_repo.go wraps the sqlc-generated
// compute_backup_targets queries (WS-25). Every query is tenant-scoped via
// WithTenant at the repository seam. The encrypted_secret_json column is
// opaque to the DB; the Go service layer encrypts/decrypts via the
// process-wide AES-GCM envelope.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// BackupTargetKind values are the supported BackupTarget driver kinds.
// The Go service factory switches on these to build the right driver.
const (
	BackupTargetKindS3  = "s3"
	BackupTargetKindNFS = "nfs"
	BackupTargetKindSSH = "ssh"
)

// ComputeBackupTargetsRepository is the persistence boundary for the
// compute_backup_targets table.
type ComputeBackupTargetsRepository struct {
	q *gen.Queries
}

// NewComputeBackupTargetsRepository wraps the given sqlc queries.
func NewComputeBackupTargetsRepository(q *gen.Queries) *ComputeBackupTargetsRepository {
	return &ComputeBackupTargetsRepository{q: q}
}

// CreateComputeBackupTargetParams carries the user-controlled fields of a
// new compute_backup_targets row. TenantID is taken from the request
// context, NOT from the caller.
type CreateComputeBackupTargetParams struct {
	Name                string
	Kind                string
	Description         string
	Config              json.RawMessage
	EncryptedSecretJSON []byte
	Enabled             bool
}

// Create inserts a new compute_backup_targets row scoped to the tenant in ctx.
func (r *ComputeBackupTargetsRepository) Create(
	ctx context.Context,
	arg CreateComputeBackupTargetParams,
) (gen.ComputeBackupTarget, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeBackupTarget{}, err
	}
	cfg := arg.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	secret := arg.EncryptedSecretJSON
	if secret == nil {
		secret = []byte{}
	}
	return r.q.CreateComputeBackupTarget(ctx, gen.CreateComputeBackupTargetParams{
		TenantID:            tenantID,
		Name:                arg.Name,
		Kind:                arg.Kind,
		Description:         arg.Description,
		ConfigJson:          cfg,
		EncryptedSecretJson: secret,
		Enabled:             arg.Enabled,
	})
}

// Get returns the compute_backup_targets row with id within the tenant in ctx.
func (r *ComputeBackupTargetsRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeBackupTarget, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeBackupTarget{}, err
	}
	return r.q.GetComputeBackupTargetByID(ctx, gen.GetComputeBackupTargetByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_backup_targets row matching name within
// the tenant in ctx.
func (r *ComputeBackupTargetsRepository) GetByName(ctx context.Context, name string) (gen.ComputeBackupTarget, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeBackupTarget{}, err
	}
	return r.q.GetComputeBackupTargetByName(ctx, gen.GetComputeBackupTargetByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// List returns a page of compute_backup_targets within the tenant in ctx.
func (r *ComputeBackupTargetsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeBackupTarget, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeBackupTargets(ctx, gen.ListComputeBackupTargetsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted compute_backup_targets within
// the tenant in ctx.
func (r *ComputeBackupTargetsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeBackupTargets(ctx, tenantID)
}

// UpdateComputeBackupTargetParams carries the user-editable fields. The
// encrypted_secret_json column is updated separately via SetSecret so a
// config edit does not require re-uploading the credentials.
type UpdateComputeBackupTargetParams struct {
	Name        string
	Description string
	Config      json.RawMessage
	Enabled     bool
}

// Update replaces the user-editable fields.
func (r *ComputeBackupTargetsRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	arg UpdateComputeBackupTargetParams,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	cfg := arg.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return r.q.UpdateComputeBackupTarget(ctx, gen.UpdateComputeBackupTargetParams{
		TenantID:    tenantID,
		ID:          id,
		Name:        arg.Name,
		Description: arg.Description,
		ConfigJson:  cfg,
		Enabled:     arg.Enabled,
	})
}

// SetSecret replaces the AES-GCM-encrypted credentials envelope.
func (r *ComputeBackupTargetsRepository) SetSecret(
	ctx context.Context,
	id uuid.UUID,
	encryptedSecretJSON []byte,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if encryptedSecretJSON == nil {
		encryptedSecretJSON = []byte{}
	}
	return r.q.SetComputeBackupTargetSecret(ctx, gen.SetComputeBackupTargetSecretParams{
		TenantID: tenantID, ID: id, EncryptedSecretJson: encryptedSecretJSON,
	})
}

// SoftDelete marks the target as deleted + disabled so the worker stops
// queueing new backups to it. The row is retained for historical joins.
func (r *ComputeBackupTargetsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeBackupTarget(ctx, gen.SoftDeleteComputeBackupTargetParams{
		TenantID: tenantID, ID: id,
	})
}
