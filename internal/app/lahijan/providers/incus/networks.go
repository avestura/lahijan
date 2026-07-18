// Package incus: networks.go wraps the Incus networks API plus the project-
// scoped sub-resources: ACLs, forwards, and (DNS) zones. All are project-
// scoped when the parent project has features.networks=true.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// CreateNetwork creates a network in the given project.
func (p *Provider) CreateNetwork(ctx context.Context, project string, body NetworksPost) error {
	ctx, span := startSpan(ctx, "network.create", projectAttr(project))
	defer span.End()
	body.Project = project
	_, err := p.do(ctx, "POST", "networks", body)
	setStatus(span, err)
	return err
}

// GetNetwork fetches a single network.
func (p *Provider) GetNetwork(ctx context.Context, project, name string) (*Network, error) {
	ctx, span := startSpan(ctx, "network.get",
		projectAttr(project), attribute.String("incus.network", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", networkPath(project, name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var net Network
	if err := json.Unmarshal(raw, &net); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode network: %w", err)
	}
	setStatus(span, nil)
	return &net, nil
}

// ListNetworks lists every network in the given project.
func (p *Provider) ListNetworks(ctx context.Context, project string) ([]Network, error) {
	ctx, span := startSpan(ctx, "network.list", projectAttr(project))
	defer span.End()
	raw, err := p.do(ctx, "GET", "networks?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []Network
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode networks: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// UpdateNetwork replaces a network's config + description.
func (p *Provider) UpdateNetwork(ctx context.Context, project, name string, body NetworkPut) error {
	ctx, span := startSpan(ctx, "network.update",
		projectAttr(project), attribute.String("incus.network", name))
	defer span.End()
	_, err := p.do(ctx, "PUT", networkPath(project, name), body)
	setStatus(span, err)
	return err
}

// DeleteNetwork removes a network from its project.
func (p *Provider) DeleteNetwork(ctx context.Context, project, name string) error {
	ctx, span := startSpan(ctx, "network.delete",
		projectAttr(project), attribute.String("incus.network", name))
	defer span.End()
	_, err := p.do(ctx, "DELETE", networkPath(project, name), nil)
	setStatus(span, err)
	return err
}

// ---- ACLs ----

// CreateNetworkACL creates a project-scoped ACL.
func (p *Provider) CreateNetworkACL(ctx context.Context, project, name string, ingress, egress []map[string]any) error {
	ctx, span := startSpan(ctx, "network_acl.create", projectAttr(project))
	defer span.End()
	body := struct {
		Name    string           `json:"name"`
		Ingress []map[string]any `json:"ingress,omitempty"`
		Egress  []map[string]any `json:"egress,omitempty"`
		Project string           `json:"project"`
	}{
		Name: name, Ingress: ingress, Egress: egress, Project: project,
	}
	_, err := p.do(ctx, "POST", "network-acls", body)
	setStatus(span, err)
	return err
}

// GetNetworkACL fetches a project-scoped ACL.
func (p *Provider) GetNetworkACL(ctx context.Context, project, name string) (*NetworkACL, error) {
	ctx, span := startSpan(ctx, "network_acl.get",
		projectAttr(project), attribute.String("incus.acl", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", "network-acls/"+url.QueryEscape(name)+"?project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var acl NetworkACL
	if err := json.Unmarshal(raw, &acl); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode network acl: %w", err)
	}
	setStatus(span, nil)
	return &acl, nil
}

// ListNetworkACLs lists every ACL in the given project.
func (p *Provider) ListNetworkACLs(ctx context.Context, project string) ([]NetworkACL, error) {
	ctx, span := startSpan(ctx, "network_acl.list", projectAttr(project))
	defer span.End()
	raw, err := p.do(ctx, "GET", "network-acls?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []NetworkACL
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode network acls: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// DeleteNetworkACL removes an ACL from its project.
func (p *Provider) DeleteNetworkACL(ctx context.Context, project, name string) error {
	ctx, span := startSpan(ctx, "network_acl.delete",
		projectAttr(project), attribute.String("incus.acl", name))
	defer span.End()
	_, err := p.do(ctx, "DELETE", "network-acls/"+url.QueryEscape(name)+"?project="+url.QueryEscape(project), nil)
	setStatus(span, err)
	return err
}

// ---- Forwards ----

// CreateNetworkForward creates a port forward on the given network.
func (p *Provider) CreateNetworkForward(ctx context.Context, project, network, listenAddress string, ports []map[string]any) error {
	ctx, span := startSpan(ctx, "network_forward.create",
		projectAttr(project), attribute.String("incus.network", network))
	defer span.End()
	body := struct {
		ListenAddress string           `json:"listen_address"`
		Ports         []map[string]any `json:"ports,omitempty"`
		Project       string           `json:"project"`
		Network       string           `json:"network"`
	}{
		ListenAddress: listenAddress, Ports: ports, Project: project, Network: network,
	}
	_, err := p.do(ctx, "POST", "networks/"+url.QueryEscape(network)+"/forwards?project="+url.QueryEscape(project), body)
	setStatus(span, err)
	return err
}

// ListNetworkForwards lists every forward on the given network.
func (p *Provider) ListNetworkForwards(ctx context.Context, project, network string) ([]NetworkForward, error) {
	ctx, span := startSpan(ctx, "network_forward.list",
		projectAttr(project), attribute.String("incus.network", network))
	defer span.End()
	raw, err := p.do(ctx, "GET", "networks/"+url.QueryEscape(network)+"/forwards?recursion=1&project="+url.QueryEscape(project), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var items []NetworkForward
	if err := json.Unmarshal(raw, &items); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode network forwards: %w", err)
	}
	setStatus(span, nil)
	return items, nil
}

// DeleteNetworkForward removes a port forward from its network.
func (p *Provider) DeleteNetworkForward(ctx context.Context, project, network, listenAddress string) error {
	ctx, span := startSpan(ctx, "network_forward.delete",
		projectAttr(project), attribute.String("incus.network", network))
	defer span.End()
	_, err := p.do(ctx, "DELETE",
		"networks/"+url.QueryEscape(network)+"/forwards/"+url.QueryEscape(listenAddress)+"?project="+url.QueryEscape(project),
		nil)
	setStatus(span, err)
	return err
}

func networkPath(project, name string) string {
	return "networks/" + url.QueryEscape(name) + "?project=" + url.QueryEscape(project)
}
