// Package compute: images.go implements the image catalog surface (list,
// get, custom upload, delete). Featured images are seeded at bootstrap from
// conf.providers.incus.featuredImages; this file's SeedFeatured path is the
// one program.Start calls once per tenant.
package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// ImageRow is the API-facing shape returned by Get/List.
type ImageRow = database.ComputeImage

// ListImages returns a paginated list of the tenant's images (featured +
// custom). The daemon is NOT probed here; the row is the cache.
func (s *Service) ListImages(
	ctx context.Context,
	limit, offset int32,
) ([]ImageRow, error) {
	return s.repos.ComputeImages.List(ctx, limit, offset)
}

// CountImages returns the number of images in the tenant.
func (s *Service) CountImages(ctx context.Context) (int64, error) {
	return s.repos.ComputeImages.Count(ctx)
}

// GetImage returns the image with id within the tenant.
func (s *Service) GetImage(ctx context.Context, id uuid.UUID) (ImageRow, error) {
	row, err := s.repos.ComputeImages.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ImageRow{}, ErrImageNotFound
		}
		return ImageRow{}, fmt.Errorf("compute: get image: %w", err)
	}
	return row, nil
}

// UploadImageParams carries the user-controlled fields of an image upload.
// Source defaults to "custom" (featured images are seeded by the operator).
type UploadImageParams struct {
	Alias        string
	Fingerprint  string
	Type         string
	Architecture string
	SizeBytes    int64
	Properties   map[string]string
	Description  string
}

// UploadImage records a custom image. The actual upload to the Incus image
// store is performed by the provider driver (TODO: WS-14 follow-up wires
// the upload call); this WS only persists the catalog row so the API can
// list + resolve the alias to a fingerprint at instance-create time.
func (s *Service) UploadImage(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params UploadImageParams,
) (ImageRow, error) {
	if params.Alias == "" {
		return ImageRow{}, ErrInvalidName
	}
	if params.Fingerprint == "" {
		return ImageRow{}, errors.New("compute: image fingerprint is required")
	}
	props, _ := json.Marshal(params.Properties)
	row, err := s.repos.ComputeImages.Create(ctx, database.CreateComputeImageParams{
		Alias:        params.Alias,
		Source:       database.ImageSourceCustom,
		Fingerprint:  params.Fingerprint,
		Type:         params.Type,
		Architecture: params.Architecture,
		SizeBytes:    params.SizeBytes,
		Properties:   props,
		Description:  params.Description,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return ImageRow{}, fmt.Errorf("%w: alias=%s", ErrNameTaken, params.Alias)
		}
		return ImageRow{}, fmt.Errorf("compute: upload image: %w", err)
	}

	_, _ = s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.image.upload",
		ResourceType: ResourceImage,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"alias":       row.Alias,
			"fingerprint": row.Fingerprint,
		},
	})
	return row, nil
}

// DeleteImage marks the image as deleted. Featured images cannot be deleted
// (they are managed by the operator); the handler returns 403 for those.
func (s *Service) DeleteImage(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	id uuid.UUID,
) error {
	row, err := s.repos.ComputeImages.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrImageNotFound
		}
		return fmt.Errorf("compute: get image: %w", err)
	}
	if row.Source == database.ImageSourceFeatured {
		return errors.New("compute: featured images cannot be deleted")
	}
	if err := s.repos.ComputeImages.SoftDelete(ctx, id); err != nil {
		return fmt.Errorf("compute: delete image: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.image.delete",
		ResourceType: ResourceImage,
		ResourceID:   &id,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// SeedFeaturedImages inserts the configured featured-image aliases into the
// tenant's catalog. Idempotent: a second call for the same tenant is a
// no-op (the unique (tenant_id, alias) constraint short-circuits the
// insert). Called from program.Start when the Incus provider is enabled.
func (s *Service) SeedFeaturedImages(
	ctx context.Context,
	tenantID uuid.UUID,
	aliases []string,
) error {
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		_, err := s.repos.ComputeImages.Create(ctx, database.CreateComputeImageParams{
			Alias:  alias,
			Source: database.ImageSourceFeatured,
		})
		if err != nil {
			if database.IsUniqueViolation(err) {
				continue
			}
			return fmt.Errorf("compute: seed featured image %q: %w", alias, err)
		}
	}
	return nil
}
