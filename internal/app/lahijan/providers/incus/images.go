// Package incus: images.go wraps the Incus images API. Image aliases
// ("ubuntu/24.04") are the user-facing identifiers; fingerprints (sha256) are
// the canonical Incus identifier.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// ListImages returns every image visible to the given project. Public images
// are returned when the daemon exposes the public catalog; project-scoped
// images require features.images=true on the project.
func (p *Provider) ListImages(ctx context.Context, project string, public bool) ([]Image, error) {
	ctx, span := startSpan(ctx, "image.list", projectAttr(project))
	defer span.End()
	q := url.Values{}
	q.Set("recursion", "1")
	if project != "" {
		q.Set("project", project)
	}
	if public {
		q.Set("public", "1")
	}
	raw, err := p.do(ctx, "GET", "images?"+q.Encode(), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []Image
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode images: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// GetImage returns the image with the given fingerprint. Use ResolveAlias to
// look up by alias first.
func (p *Provider) GetImage(ctx context.Context, project, fingerprint string) (*Image, error) {
	ctx, span := startSpan(ctx, "image.get",
		projectAttr(project), attribute.String("incus.image_fingerprint", fingerprint))
	defer span.End()
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	path := "images/" + url.QueryEscape(fingerprint)
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	raw, err := p.do(ctx, "GET", path, nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var img Image
	if err := json.Unmarshal(raw, &img); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode image: %w", err)
	}
	setStatus(span, nil)
	return &img, nil
}

// ResolveAlias returns the image fingerprint the given alias points at within
// the given project (or the public catalog when project is empty).
func (p *Provider) ResolveAlias(ctx context.Context, project, alias string) (string, error) {
	ctx, span := startSpan(ctx, "image.alias.resolve",
		projectAttr(project), attribute.String("incus.image_alias", alias))
	defer span.End()
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	path := "images/aliases/" + url.QueryEscape(alias)
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	raw, err := p.do(ctx, "GET", path, nil)
	if err != nil {
		setStatus(span, err)
		return "", err
	}
	var resp struct {
		Name        string `json:"name"`
		Target      string `json:"target"`
		Description string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		setStatus(span, err)
		return "", fmt.Errorf("incus: decode image alias: %w", err)
	}
	setStatus(span, nil)
	if resp.Target == "" {
		return "", fmt.Errorf("incus: alias %q resolved to empty target", alias)
	}
	return resp.Target, nil
}

// CopyImageParams controls a copy-image operation.
type CopyImageParams struct {
	// Project is the destination project (the source project defaults to the
	// same when SourceProject is empty).
	Project string

	// Source is the source descriptor.
	Source ImageSource

	// Aliases are the aliases to register in the destination project.
	Aliases []ImageAlias

	// Public marks the copy as public (visible to non-members).
	Public bool

	// AutoUpdate toggles the daemon's auto-refresh of the copy.
	AutoUpdate bool
}

// CopyImage copies an image (from a remote server or another project) and
// waits for the operation to complete.
func (p *Provider) CopyImage(ctx context.Context, params CopyImageParams) (*Operation, error) {
	ctx, span := startSpan(ctx, "image.copy", projectAttr(params.Project))
	defer span.End()
	body := ImagesPost{
		Aliases:    params.Aliases,
		Public:     params.Public,
		AutoUpdate: params.AutoUpdate,
		Project:    params.Project,
		Source:     params.Source,
	}
	op, err := p.doAsync(ctx, "POST", "images", body)
	setStatus(span, err)
	return op, err
}

// DeleteImage removes an image from the daemon. The image must not be in use
// by any instance.
func (p *Provider) DeleteImage(ctx context.Context, project, fingerprint string) (*Operation, error) {
	ctx, span := startSpan(ctx, "image.delete", projectAttr(project))
	defer span.End()
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	path := "images/" + url.QueryEscape(fingerprint)
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	op, err := p.doAsync(ctx, "DELETE", path, nil)
	setStatus(span, err)
	return op, err
}

// FeaturedImages returns the list of image aliases advertised as "featured" by
// Lahijan's config (conf.providers.incus.featuredImages). The list is the
// catalog the compute module's image picker shows to the user; the daemon is
// expected to have these images available via its image server.
//
// This is a pure data call (no daemon round-trip); the compute module uses it
// to render the picker before any daemon healthcheck has run.
func FeaturedImages(cfg []string) []string {
	out := make([]string, 0, len(cfg))
	for _, a := range cfg {
		if a == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}
