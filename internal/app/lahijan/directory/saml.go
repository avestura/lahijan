package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// SAMLActivator makes SAML connections live in the sign-in registry. The
// bootstrap implements it (it owns the service-provider credentials); the
// directory service calls it whenever a SAML connection is created, changed
// or removed, so a connection signs users in without a restart.
type SAMLActivator interface {
	// Activate builds the provider for a connection and registers it under
	// name. It returns an error when the provider cannot be built (for
	// example unreachable IdP metadata or no service-provider signing key).
	Activate(name string, cfg SAMLConfig) error
	// Deactivate removes the provider registered under name.
	Deactivate(name string)
	// IsActive reports whether a provider is registered under name.
	IsActive(name string) bool
}

// SetSAMLActivator wires the activator. Without one, SAML connections are only
// stored and tested (they never become active).
func (s *Service) SetSAMLActivator(a SAMLActivator) { s.saml = a }

// ActivateSAML registers every enabled SAML connection. The bootstrap calls it
// once at startup; failures are logged and leave that connection inactive.
func (s *Service) ActivateSAML(ctx context.Context) {
	if s.saml == nil {
		return
	}
	rows, err := s.dir.ListConnections(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "directory: list connections for SAML activation failed", "err", err)
		return
	}
	for _, r := range rows {
		if r.Kind != KindSAML || !r.Enabled {
			continue
		}
		if err := s.activateRow(r); err != nil {
			s.log.WarnContext(ctx, "directory: SAML connection not activated", "connection", r.Name, "err", err)
		}
	}
}

// activateRow activates one SAML row.
func (s *Service) activateRow(r gen.DirectoryConnection) error {
	var cfg SAMLConfig
	if err := json.Unmarshal(r.Config, &cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return s.saml.Activate(r.Name, cfg)
}

// applySAMLChange reconciles the registry after a create / update / delete.
// old is the row before the change (nil on create), cur the row after (nil on
// delete). It returns the activation error, if any, so the caller can show it.
func (s *Service) applySAMLChange(old, cur *gen.DirectoryConnection) error {
	if s.saml == nil {
		return nil
	}
	if old != nil && old.Kind == KindSAML && (cur == nil || cur.Name != old.Name) {
		s.saml.Deactivate(old.Name)
	}
	if cur == nil || cur.Kind != KindSAML {
		return nil
	}
	if !cur.Enabled {
		s.saml.Deactivate(cur.Name)
		return nil
	}
	return s.activateRow(*cur)
}

// decorate fills the runtime-only fields of a connection view.
func (s *Service) decorate(c Connection) Connection {
	switch c.Kind {
	case KindSAML:
		c.Active = c.Enabled && s.saml != nil && s.saml.IsActive(c.Name)
	default:
		c.Active = c.Enabled
	}
	return c
}

// RecordSAMLLogin records the directory side of a successful SAML sign-in: it
// links the user to the connection and refreshes the groups the assertion
// carried (read from the connection's groups attribute). It is best-effort and
// a no-op for providers that are not a directory connection (for instance ones
// defined in the config file).
func (s *Service) RecordSAMLLogin(ctx context.Context, providerKey string, userID uuid.UUID, nameID string, attrs map[string]any) {
	row, err := s.dir.GetConnectionByName(ctx, providerKey)
	if err != nil {
		if !database.IsNoRows(err) {
			s.log.WarnContext(ctx, "directory: look up SAML connection failed", "provider", providerKey, "err", err)
		}
		return
	}
	if row.Kind != KindSAML {
		return
	}
	if nameID != "" {
		if err := s.dir.LinkUser(ctx, row.ID, userID, nameID); err != nil {
			s.log.WarnContext(ctx, "directory: link SAML user failed", "connection", row.Name, "err", err)
		}
	}
	var cfg SAMLConfig
	if err := json.Unmarshal(row.Config, &cfg); err != nil || strings.TrimSpace(cfg.GroupsAttribute) == "" {
		return
	}
	groups := stringValues(attrs[cfg.GroupsAttribute])
	if err := s.dir.RemoveUserFromConnectionGroups(ctx, row.ID, userID); err != nil {
		s.log.WarnContext(ctx, "directory: clear SAML groups failed", "connection", row.Name, "err", err)
		return
	}
	for _, g := range groups {
		grp, err := s.dir.UpsertGroup(ctx, row.ID, g, g, "")
		if err != nil {
			s.log.WarnContext(ctx, "directory: record SAML group failed", "connection", row.Name, "group", g, "err", err)
			continue
		}
		if err := s.dir.AddUserToGroup(ctx, grp.ID, userID); err != nil {
			s.log.WarnContext(ctx, "directory: add SAML group member failed", "connection", row.Name, "group", g, "err", err)
		}
	}
}

// stringValues flattens a SAML attribute value ([]string, []any or string)
// into trimmed, non-empty strings.
func stringValues(v any) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	switch t := v.(type) {
	case string:
		add(t)
	case []string:
		for _, s := range t {
			add(s)
		}
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				add(s)
			}
		}
	}
	return out
}
