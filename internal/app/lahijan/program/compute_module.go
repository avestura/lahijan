// Package program: compute_module.go wires the compute module (WS-14) into
// process bootstrap. The compute Service is built once the Incus provider is
// available; the seed helper walks every existing tenant and inserts the
// featured-image catalog rows so the per-tenant picker renders without a
// manual one-time bootstrap.
package program

import (
	"context"
	"log/slog"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// seedComputeFeaturedImagesForTenants walks every non-deleted tenant and
// inserts the featured-image catalog rows. Idempotent: a second call for
// the same tenant is a no-op (the unique (tenant_id, alias) constraint
// short-circuits the insert). Errors are logged at Warn but do not fail
// bootstrap; a missing row just means the picker does not show that alias.
func seedComputeFeaturedImagesForTenants(
	ctx context.Context,
	svc *compute.Service,
	tenants *database.TenantsRepository,
	aliases []string,
) {
	if svc == nil || tenants == nil || len(aliases) == 0 {
		return
	}
	const pageSize = 100
	offset := int32(0)
	for {
		page, err := tenants.List(ctx, pageSize, offset)
		if err != nil {
			slog.Default().Warn("compute featured-image seed: list tenants failed",
				"offset", offset, "error", err.Error())
			return
		}
		for _, t := range page {
			if err := svc.SeedFeaturedImages(ctx, t.ID, aliases); err != nil {
				slog.Default().Warn("compute featured-image seed: tenant failed",
					"tenant_id", t.ID, "error", err.Error())
			}
		}
		if len(page) < pageSize {
			return
		}
		offset += pageSize
	}
}
