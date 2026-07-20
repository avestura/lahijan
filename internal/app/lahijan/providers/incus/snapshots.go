// Package incus: snapshots.go wraps the Incus instance-snapshots API. Every
// method takes an explicit project + instance name; the caller (typically
// the compute service in WS-25) resolves tenant -> project before calling.
// All methods open an OTel span; create/delete/restore return the final
// Operation state.
//
// Reference: https://linuxcontainers.org/incus/docs/main/rest-api-spec/
// (paths under /1.0/instances/<name>/snapshots/*).
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// CreateSnapshotParams is the user-visible shape of a create-snapshot call.
type CreateSnapshotParams struct {
	// Project is the Incus project name (the caller maps tenant -> project).
	Project string
	// Instance is the instance name within the project.
	Instance string
	// Name is the snapshot name; unique within the instance.
	Name string
	// Stateful captures runtime state alongside the filesystem.
	Stateful bool
}

// CreateSnapshot creates an instance snapshot and waits for the operation
// to complete. Returns the final Operation state.
func (p *Provider) CreateSnapshot(ctx context.Context, params CreateSnapshotParams) (*Operation, error) {
	ctx, span := startSpan(ctx, "snapshot.create",
		projectAttr(params.Project), attribute.String("incus.instance", params.Instance),
		attribute.String("incus.snapshot", params.Name))
	defer span.End()
	body := InstanceSnapshotsPost{
		Name:     params.Name,
		Stateful: params.Stateful,
	}
	op, err := p.doAsync(ctx, "POST", instanceSnapshotsPath(params.Project, params.Instance), body)
	setStatus(span, err)
	return op, err
}

// ListInstanceSnapshots lists every snapshot of the given instance.
func (p *Provider) ListInstanceSnapshots(ctx context.Context, project, instance string) ([]InstanceSnapshot, error) {
	ctx, span := startSpan(ctx, "snapshot.list",
		projectAttr(project), attribute.String("incus.instance", instance))
	defer span.End()
	raw, err := p.do(ctx, "GET", instanceSnapshotsPath(project, instance)+"&recursion=1", nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []InstanceSnapshot
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode snapshots: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// GetSnapshot fetches a single snapshot's metadata.
func (p *Provider) GetSnapshot(ctx context.Context, project, instance, snapshot string) (*InstanceSnapshot, error) {
	ctx, span := startSpan(ctx, "snapshot.get",
		projectAttr(project), attribute.String("incus.instance", instance),
		attribute.String("incus.snapshot", snapshot))
	defer span.End()
	raw, err := p.do(ctx, "GET", snapshotPath(project, instance, snapshot), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var snap InstanceSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode snapshot: %w", err)
	}
	setStatus(span, nil)
	return &snap, nil
}

// RenameSnapshot renames an existing snapshot. Returns the final operation
// state (Incus processes renames asynchronously).
func (p *Provider) RenameSnapshot(
	ctx context.Context, project, instance, snapshot, newName string,
) (*Operation, error) {
	ctx, span := startSpan(ctx, "snapshot.rename",
		projectAttr(project), attribute.String("incus.instance", instance),
		attribute.String("incus.snapshot", snapshot))
	defer span.End()
	op, err := p.doAsync(ctx, "POST", snapshotPath(project, instance, snapshot),
		InstanceSnapshotPut{Name: newName})
	setStatus(span, err)
	return op, err
}

// DeleteSnapshot deletes an instance snapshot and waits for the operation
// to complete.
func (p *Provider) DeleteSnapshot(ctx context.Context, project, instance, snapshot string) (*Operation, error) {
	ctx, span := startSpan(ctx, "snapshot.delete",
		projectAttr(project), attribute.String("incus.instance", instance),
		attribute.String("incus.snapshot", snapshot))
	defer span.End()
	op, err := p.doAsync(ctx, "DELETE", snapshotPath(project, instance, snapshot), nil)
	setStatus(span, err)
	return op, err
}

// RestoreSnapshot restores an instance to a prior snapshot. The instance
// must already exist in the project (Incus does not auto-create it).
// Returns the final operation state.
func (p *Provider) RestoreSnapshot(
	ctx context.Context, project, instance, snapshot string, stateful bool,
) (*Operation, error) {
	ctx, span := startSpan(ctx, "snapshot.restore",
		projectAttr(project), attribute.String("incus.instance", instance),
		attribute.String("incus.snapshot", snapshot))
	defer span.End()
	op, err := p.doAsync(ctx, "POST",
		snapshotPathSuffix(project, instance, snapshot, "restore"),
		InstanceSnapshotRestorePost{Stateful: stateful})
	setStatus(span, err)
	return op, err
}

// ExportSnapshot downloads a snapshot's tarball bytes from the daemon.
// Returns the raw tar.gz stream the caller can pipe to a backup target.
// The Incus export endpoint is GET
// /1.0/instances/<name>/snapshots/<snap>/export; it returns the binary
// payload (no JSON envelope), so this method bypasses p.do (which expects
// the JSON envelope) and reads the body directly.
func (p *Provider) ExportSnapshot(
	ctx context.Context, project, instance, snapshot string,
) ([]byte, error) {
	ctx, span := startSpan(ctx, "snapshot.export",
		projectAttr(project), attribute.String("incus.instance", instance),
		attribute.String("incus.snapshot", snapshot))
	defer span.End()
	path := snapshotPathSuffix(project, instance, snapshot, "export")
	req, err := p.buildExportRequest(ctx, "GET", path)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return nil, fmt.Errorf("incus: export snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if classErr := classify(resp); classErr != nil {
		setStatus(span, classErr)
		return nil, fmt.Errorf("incus: export snapshot: %w", classErr)
	}
	// Read up to 16 GiB per snapshot. The limit is generous because snapshot
	// tarballs are typically the size of the instance's root disk; a stricter
	// cap would reject legitimate large VMs. A misbehaving daemon that
	// streams past 16 GiB is treated as an error.
	const maxExportBytes = 16 * 1024 * 1024 * 1024
	buf := make([]byte, 0, 1<<20)
	chunk := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if int64(len(buf)) > maxExportBytes {
				setStatus(span, fmt.Errorf("snapshot export exceeds %d bytes", maxExportBytes))
				return nil, fmt.Errorf("incus: snapshot export exceeds %d bytes", maxExportBytes)
			}
		}
		if readErr != nil {
			break
		}
	}
	setStatus(span, nil)
	return buf, nil
}

// buildExportRequest constructs the *http.Request for the binary-export
// endpoint. The Accept header is forced to application/octet-stream so the
// daemon does not negotiate JSON.
func (p *Provider) buildExportRequest(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := p.buildRequest(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	return req, nil
}

// instanceSnapshotsPath builds the snapshots-collection REST path.
func instanceSnapshotsPath(project, instance string) string {
	return "instances/" + url.QueryEscape(instance) +
		"/snapshots?project=" + url.QueryEscape(project)
}

// snapshotPath builds the per-snapshot REST path.
func snapshotPath(project, instance, snapshot string) string {
	return "instances/" + url.QueryEscape(instance) +
		"/snapshots/" + url.QueryEscape(snapshot) +
		"?project=" + url.QueryEscape(project)
}

// snapshotPathSuffix builds a per-snapshot REST path with an extra
// sub-segment (e.g. "restore", "export") appended BEFORE the project
// query string so the URL parses cleanly.
func snapshotPathSuffix(project, instance, snapshot, suffix string) string {
	return "instances/" + url.QueryEscape(instance) +
		"/snapshots/" + url.QueryEscape(snapshot) +
		"/" + suffix +
		"?project=" + url.QueryEscape(project)
}
