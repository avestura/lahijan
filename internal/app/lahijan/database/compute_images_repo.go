// Package database: compute_images_repo.go wraps the sqlc-generated
// compute_images queries (WS-14). Every query is tenant-scoped via
// WithTenant at the repository seam.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ImageSource values.
const (
	ImageSourceFeatured = "featured"
	ImageSourceCustom   = "custom"
)

// ComputeImagesRepository is the persistence boundary for compute_images.
type ComputeImagesRepository struct {
	q *gen.Queries
}

// NewComputeImagesRepository wraps the given sqlc queries.
func NewComputeImagesRepository(q *gen.Queries) *ComputeImagesRepository {
	return &ComputeImagesRepository{q: q}
}

// CreateComputeImageParams carries the user-controlled fields of a new row.
type CreateComputeImageParams struct {
	Alias          string
	Source         string
	Fingerprint    string
	Type           string
	Architecture   string
	SizeBytes      int64
	PropertiesJson json.RawMessage
	Description    string
}

// Create inserts a new compute_images row scoped to the tenant in ctx.
func (r *ComputeImagesRepository) Create(
	ctx context.Context,
	arg CreateComputeImageParams,
) (gen.ComputeImage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeImage{}, err
	}
	src := arg.Source
	if src == "" {
		src = ImageSourceFeatured
	}
	imgType := arg.Type
	if imgType == "" {
		imgType = InstanceTypeContainer
	}
	props := arg.PropertiesJson
	if len(props) == 0 {
		props = json.RawMessage(`{}`)
	}
	return r.q.CreateComputeImage(ctx, gen.CreateComputeImageParams{
		TenantID:       tenantID,
		Alias:          arg.Alias,
		Source:         src,
		Fingerprint:    arg.Fingerprint,
		Type:           imgType,
		Architecture:   arg.Architecture,
		SizeBytes:      arg.SizeBytes,
		PropertiesJson: props,
		Description:    arg.Description,
	})
}

// Get returns the compute_images row with id within the tenant in ctx.
func (r *ComputeImagesRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeImage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeImage{}, err
	}
	return r.q.GetComputeImageByID(ctx, gen.GetComputeImageByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByAlias returns the compute_images row matching alias within the
// tenant in ctx. Used by the instance-create path to resolve a user-supplied
// alias to a fingerprint.
func (r *ComputeImagesRepository) GetByAlias(
	ctx context.Context,
	alias string,
) (gen.ComputeImage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeImage{}, err
	}
	return r.q.GetComputeImageByAlias(ctx, gen.GetComputeImageByAliasParams{
		TenantID: tenantID, Alias: alias,
	})
}

// GetByFingerprint returns the compute_images row matching fingerprint
// within the tenant in ctx. Used by the upload path to detect duplicates.
func (r *ComputeImagesRepository) GetByFingerprint(
	ctx context.Context,
	fingerprint string,
) (gen.ComputeImage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeImage{}, err
	}
	return r.q.GetComputeImageByFingerprint(ctx, gen.GetComputeImageByFingerprintParams{
		TenantID: tenantID, Fingerprint: fingerprint,
	})
}

// List returns a page of compute_images within the tenant in ctx.
func (r *ComputeImagesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeImage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeImages(ctx, gen.ListComputeImagesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of compute_images within the tenant in ctx.
func (r *ComputeImagesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeImages(ctx, tenantID)
}

// UpsertFingerprint sets the fingerprint on an existing row. Called by the
// bootstrap path after the daemon resolves a featured alias lazily.
func (r *ComputeImagesRepository) UpsertFingerprint(
	ctx context.Context,
	alias, fingerprint string,
	sizeBytes int64,
	properties json.RawMessage,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if len(properties) == 0 {
		properties = json.RawMessage(`{}`)
	}
	return r.q.UpsertComputeImageFingerprint(ctx, gen.UpsertComputeImageFingerprintParams{
		TenantID:       tenantID,
		Alias:          alias,
		Fingerprint:    fingerprint,
		SizeBytes:      sizeBytes,
		PropertiesJson: properties,
	})
}

// SoftDelete marks the image as deleted.
func (r *ComputeImagesRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeImage(ctx, gen.SoftDeleteComputeImageParams{
		TenantID: tenantID, ID: id,
	})
}
