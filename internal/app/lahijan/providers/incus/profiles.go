// Package incus: profiles.go wraps the Incus profiles API. Profiles are
// project-scoped bundles of config + devices applied to instances; the compute
// module uses them to give tenants pre-baked shapes ("small", "gpu", "public-ip").
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// CreateProfileParams is the user-visible shape of a profile-create call.
type CreateProfileParams struct {
	Project     string
	Name        string
	Description string
	Config      map[string]string
	Devices     map[string]map[string]string
}

// CreateProfile creates a profile in the given project. Returns
// ErrAlreadyExists if a profile with the same name already exists in the
// project — callers wishing to be idempotent should use EnsureProfile.
func (p *Provider) CreateProfile(ctx context.Context, params CreateProfileParams) error {
	ctx, span := startSpan(ctx, "profile.create", projectAttr(params.Project))
	defer span.End()
	body := ProfilesPost{
		Name:        params.Name,
		Description: params.Description,
		Config:      params.Config,
		Devices:     params.Devices,
		Project:     params.Project,
	}
	_, err := p.do(ctx, "POST", "profiles", body)
	setStatus(span, err)
	return err
}

// EnsureProfile creates the profile if it does not exist, or updates it in
// place if it does. Idempotent — used by the compute service when seeding
// default profile templates per tenant.
func (p *Provider) EnsureProfile(ctx context.Context, params CreateProfileParams) error {
	err := p.CreateProfile(ctx, params)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrAlreadyExists) {
		return err
	}
	return p.UpdateProfile(ctx, params.Project, params.Name, ProfilePut{
		Description: params.Description,
		Config:      params.Config,
		Devices:     params.Devices,
	})
}

// GetProfile fetches a single profile.
func (p *Provider) GetProfile(ctx context.Context, project, name string) (*Profile, error) {
	ctx, span := startSpan(ctx, "profile.get", projectAttr(project))
	defer span.End()
	raw, err := p.do(ctx, "GET", profilePath(project, name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var prf Profile
	if err := json.Unmarshal(raw, &prf); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode profile: %w", err)
	}
	setStatus(span, nil)
	return &prf, nil
}

// ListProfiles lists every profile in the given project.
func (p *Provider) ListProfiles(ctx context.Context, project string) ([]Profile, error) {
	ctx, span := startSpan(ctx, "profile.list", projectAttr(project))
	defer span.End()
	raw, err := p.do(ctx, "GET", "profiles?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []Profile
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode profiles: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// UpdateProfile replaces a profile's config + devices.
func (p *Provider) UpdateProfile(ctx context.Context, project, name string, body ProfilePut) error {
	ctx, span := startSpan(ctx, "profile.update", projectAttr(project))
	defer span.End()
	_, err := p.do(ctx, "PUT", profilePath(project, name), body)
	setStatus(span, err)
	return err
}

// DeleteProfile removes a profile from its project.
func (p *Provider) DeleteProfile(ctx context.Context, project, name string) error {
	ctx, span := startSpan(ctx, "profile.delete", projectAttr(project))
	defer span.End()
	_, err := p.do(ctx, "DELETE", profilePath(project, name), nil)
	setStatus(span, err)
	return err
}

func profilePath(project, name string) string {
	return "profiles/" + url.QueryEscape(name) + "?project=" + url.QueryEscape(project)
}
