// Package database: platform_settings_repo.go wraps the sqlc queries for the
// runtime platform settings (global table; see migration 0051).
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// PlatformSettingsRepository is the persistence boundary for platform_settings.
type PlatformSettingsRepository struct {
	q *gen.Queries
}

// NewPlatformSettingsRepository wraps the given sqlc queries.
func NewPlatformSettingsRepository(q *gen.Queries) *PlatformSettingsRepository {
	return &PlatformSettingsRepository{q: q}
}

// GetBool returns a boolean setting. ok is false when no value is stored (or
// the stored value is not a boolean).
func (r *PlatformSettingsRepository) GetBool(ctx context.Context, key string) (value, ok bool, err error) {
	row, err := r.q.GetPlatformSetting(ctx, key)
	if err != nil {
		if IsNoRows(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if jsonErr := json.Unmarshal(row.Value, &value); jsonErr != nil {
		return false, false, nil //nolint:nilerr // a malformed value is treated as unset
	}
	return value, true, nil
}

// Set stores a setting, replacing any previous value.
func (r *PlatformSettingsRepository) Set(ctx context.Context, key string, value json.RawMessage, updatedBy *uuid.UUID) error {
	_, err := r.q.UpsertPlatformSetting(ctx, gen.UpsertPlatformSettingParams{Key: key, Value: value, UpdatedBy: updatedBy})
	return err
}

// Delete removes a stored setting so the configured default applies again.
func (r *PlatformSettingsRepository) Delete(ctx context.Context, key string) error {
	return r.q.DeletePlatformSetting(ctx, key)
}
