// Package incus: instances.go wraps the Incus instances API. Every method
// takes an explicit project name (the caller — typically the compute service
// in WS-14 — maps a tenant to its project before calling). All methods open
// an OTel span; create/start/stop/restart/delete return the final Operation
// state.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// CreateInstanceParams is the user-visible shape of an instance-create call.
// Source.Alias selects the image; if you already have a fingerprint, use
// Source.Fingerprint instead.
type CreateInstanceParams struct {
	// Project is the Incus project name (the caller maps tenant -> project).
	Project string

	// Name is the instance name; if empty Incus generates one.
	Name string

	// Type is "container" or "virtual-machine". Empty defaults to
	// "container" on the daemon side.
	Type string

	// Description is the user-visible description.
	Description string

	// Config is the instance's config map.
	Config map[string]string

	// Devices is the per-device config map.
	Devices map[string]map[string]string

	// Profiles is the list of profiles applied to the instance.
	Profiles []string

	// Source is the image source (alias or fingerprint).
	Source InstanceSource
}

// CreateInstance creates an instance and waits for the operation to complete
// (the daemon's async response is resolved via the operations API). Returns
// the final Operation state.
func (p *Provider) CreateInstance(ctx context.Context, params CreateInstanceParams) (*Operation, error) {
	ctx, span := startSpan(ctx, "instance.create", projectAttr(params.Project))
	defer span.End()
	body := InstancesPost{
		Name:         params.Name,
		Project:      params.Project,
		Type:         params.Type,
		Description:  params.Description,
		Config:       params.Config,
		Devices:      params.Devices,
		Profiles:     params.Profiles,
		Architecture: "x86_64",
		Source:       params.Source,
	}
	op, err := p.doAsync(ctx, "POST", "instances", body)
	setStatus(span, err)
	return op, err
}

// GetInstance fetches an instance's current state (config, status, devices).
func (p *Provider) GetInstance(ctx context.Context, project, name string) (*Instance, error) {
	ctx, span := startSpan(ctx, "instance.get",
		projectAttr(project), attribute.String("incus.instance", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", instancePath(project, name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var inst Instance
	if err := json.Unmarshal(raw, &inst); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode instance: %w", err)
	}
	setStatus(span, nil)
	return &inst, nil
}

// ListInstances lists every instance in the given project.
func (p *Provider) ListInstances(ctx context.Context, project string) ([]Instance, error) {
	ctx, span := startSpan(ctx, "instance.list", projectAttr(project))
	defer span.End()
	raw, err := p.do(ctx, "GET", "instances?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []Instance
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode instances: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// InstanceAction is the lifecycle action applied to an instance.
type InstanceAction string

const (
	ActionStart    InstanceAction = "start"
	ActionStop     InstanceAction = "stop"
	ActionRestart  InstanceAction = "restart"
	ActionFreeze   InstanceAction = "freeze"
	ActionUnfreeze InstanceAction = "unfreeze"
)

// SetInstanceState applies a lifecycle action to an instance and waits for
// the operation to complete.
func (p *Provider) SetInstanceState(
	ctx context.Context, project, name string,
	action InstanceAction, force bool, timeoutSecs int,
) (*Operation, error) {
	ctx, span := startSpan(ctx, "instance.state."+string(action),
		projectAttr(project), attribute.String("incus.instance", name))
	defer span.End()
	body := InstanceStatePut{
		Action:  string(action),
		Timeout: timeoutSecs,
		Force:   force,
	}
	op, err := p.doAsync(ctx, "PUT", instanceStatePath(project, name), body)
	setStatus(span, err)
	return op, err
}

// GetInstanceState fetches the runtime state (CPU, memory, network, pid).
func (p *Provider) GetInstanceState(ctx context.Context, project, name string) (*InstanceState, error) {
	ctx, span := startSpan(ctx, "instance.state.get",
		projectAttr(project), attribute.String("incus.instance", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", instanceStatePath(project, name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var st InstanceState
	if err := json.Unmarshal(raw, &st); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode instance state: %w", err)
	}
	setStatus(span, nil)
	return &st, nil
}

// UpdateInstance replaces an instance's config + devices + profiles.
func (p *Provider) UpdateInstance(ctx context.Context, project, name string, body InstancePut) (*Operation, error) {
	ctx, span := startSpan(ctx, "instance.update",
		projectAttr(project), attribute.String("incus.instance", name))
	defer span.End()
	op, err := p.doAsync(ctx, "PUT", instancePath(project, name), body)
	setStatus(span, err)
	return op, err
}

// DeleteInstance removes an instance from its project. The instance must be
// stopped first; callers that want "force delete regardless of state" should
// stop-then-delete in a single transaction.
func (p *Provider) DeleteInstance(ctx context.Context, project, name string) (*Operation, error) {
	ctx, span := startSpan(ctx, "instance.delete",
		projectAttr(project), attribute.String("incus.instance", name))
	defer span.End()
	op, err := p.doAsync(ctx, "DELETE", instancePath(project, name), nil)
	setStatus(span, err)
	return op, err
}

// instancePath builds the instance REST path with the project query string.
func instancePath(project, name string) string {
	return "instances/" + url.QueryEscape(name) + "?project=" + url.QueryEscape(project)
}

// instanceStatePath builds the instance state REST path.
func instanceStatePath(project, name string) string {
	return "instances/" + url.QueryEscape(name) + "/state?project=" + url.QueryEscape(project)
}
