// Package incus: storage.go wraps the Incus storage API: pools (cluster-wide)
// and volumes (project-scoped). Pools are global — the Lahijan operator
// configures them; tenants see volumes within their project.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// CreateStoragePool creates a cluster-wide storage pool. Pool creation is
// admin-only in Lahijan; tenants consume volumes within an existing pool.
func (p *Provider) CreateStoragePool(ctx context.Context, body StoragePoolsPost) error {
	ctx, span := startSpan(ctx, "storage_pool.create")
	defer span.End()
	_, err := p.do(ctx, "POST", "storage-pools", body)
	setStatus(span, err)
	return err
}

// GetStoragePool fetches a single pool.
func (p *Provider) GetStoragePool(ctx context.Context, name string) (*StoragePool, error) {
	ctx, span := startSpan(ctx, "storage_pool.get",
		attribute.String("incus.pool", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", "storage-pools/"+url.QueryEscape(name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var pool StoragePool
	if err := json.Unmarshal(raw, &pool); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode storage pool: %w", err)
	}
	setStatus(span, nil)
	return &pool, nil
}

// ListStoragePools lists every cluster-wide storage pool.
func (p *Provider) ListStoragePools(ctx context.Context) ([]StoragePool, error) {
	ctx, span := startSpan(ctx, "storage_pool.list")
	defer span.End()
	raw, err := p.do(ctx, "GET", "storage-pools?recursion=1", nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []StoragePool
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode storage pools: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// UpdateStoragePool replaces a pool's config + description.
func (p *Provider) UpdateStoragePool(ctx context.Context, name string, body StoragePoolPut) error {
	ctx, span := startSpan(ctx, "storage_pool.update",
		attribute.String("incus.pool", name))
	defer span.End()
	_, err := p.do(ctx, "PUT", "storage-pools/"+url.QueryEscape(name), body)
	setStatus(span, err)
	return err
}

// DeleteStoragePool removes a pool. Pool must have no volumes left.
func (p *Provider) DeleteStoragePool(ctx context.Context, name string) error {
	ctx, span := startSpan(ctx, "storage_pool.delete",
		attribute.String("incus.pool", name))
	defer span.End()
	_, err := p.do(ctx, "DELETE", "storage-pools/"+url.QueryEscape(name), nil)
	setStatus(span, err)
	return err
}

// ---- Volumes ----

// CreateStorageVolume creates a project-scoped custom volume.
func (p *Provider) CreateStorageVolume(ctx context.Context, pool string, body StorageVolumesPost) error {
	ctx, span := startSpan(ctx, "storage_volume.create",
		projectAttr(body.Project), attribute.String("incus.pool", pool))
	defer span.End()
	_, err := p.do(ctx, "POST", "storage-pools/"+url.QueryEscape(pool)+"/volumes", body)
	setStatus(span, err)
	return err
}

// GetStorageVolume fetches a single project-scoped volume.
func (p *Provider) GetStorageVolume(ctx context.Context, pool, project, volType, name string) (*StorageVolume, error) {
	ctx, span := startSpan(ctx, "storage_volume.get",
		projectAttr(project), attribute.String("incus.pool", pool),
		attribute.String("incus.volume", name))
	defer span.End()
	path := fmt.Sprintf("storage-pools/%s/volumes/%s/%s?project=%s",
		url.QueryEscape(pool), url.QueryEscape(volType), url.QueryEscape(name), url.QueryEscape(project))
	raw, err := p.do(ctx, "GET", path, nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var vol StorageVolume
	if err := json.Unmarshal(raw, &vol); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode storage volume: %w", err)
	}
	setStatus(span, nil)
	return &vol, nil
}

// ListStorageVolumes lists every volume in the given pool + project.
func (p *Provider) ListStorageVolumes(ctx context.Context, pool, project string) ([]StorageVolume, error) {
	ctx, span := startSpan(ctx, "storage_volume.list",
		projectAttr(project), attribute.String("incus.pool", pool))
	defer span.End()
	raw, err := p.do(ctx, "GET",
		"storage-pools/"+url.QueryEscape(pool)+
			"/volumes?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []StorageVolume
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode storage volumes: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// DeleteStorageVolume removes a volume from its pool + project.
func (p *Provider) DeleteStorageVolume(ctx context.Context, pool, project, volType, name string) error {
	ctx, span := startSpan(ctx, "storage_volume.delete",
		projectAttr(project), attribute.String("incus.pool", pool),
		attribute.String("incus.volume", name))
	defer span.End()
	path := fmt.Sprintf("storage-pools/%s/volumes/%s/%s?project=%s",
		url.QueryEscape(pool), url.QueryEscape(volType), url.QueryEscape(name), url.QueryEscape(project))
	_, err := p.do(ctx, "DELETE", path, nil)
	setStatus(span, err)
	return err
}
