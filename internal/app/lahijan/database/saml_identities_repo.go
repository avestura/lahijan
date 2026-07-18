// Package database: saml_identities_repo.go wraps the sqlc-generated
// user_saml_identities queries (WS-07b). The table is global (no tenant_id)
// because an external SAML identity belongs to a user, not a tenant — login is
// platform-wide and a user can be a member of many tenants (ADR-0002,
// ADR-0004).
//
// Unlike user_oauth_identities, this table holds no encrypted tokens: SAML
// assertions are short-lived (minutes) and the only stateful piece Lahijan
// keeps is the latest attribute snapshot. The attributes_json column carries
// that snapshot as opaque JSONB; this repo never inspects or modifies the
// contents. The idp service is the sole writer.
package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// SamlIdentitiesRepository is the persistence boundary for
// user_saml_identities.
type SamlIdentitiesRepository struct {
	q *gen.Queries
}

// NewSamlIdentitiesRepository wraps the given sqlc queries.
func NewSamlIdentitiesRepository(q *gen.Queries) *SamlIdentitiesRepository {
	return &SamlIdentitiesRepository{q: q}
}

// CreateSAMLIdentityParams carries the fields of a new SAML identity link.
// AttributesJSON is the snapshot from the IdP's attribute statement; pass an
// empty (or "{}") RawMessage when the assertion carried no attributes.
type CreateSAMLIdentityParams struct {
	UserID      uuid.UUID
	Provider    string
	NameID      string
	IdpEntityID string
	Attributes  map[string]any
}

// Create inserts a new SAML identity link. A duplicate (provider, name_id)
// or (user_id, provider) raises a Postgres unique-violation the caller maps
// to a "this identity is already linked" error. Attributes is JSON-encoded
// by this method; the caller passes a structured map.
func (r *SamlIdentitiesRepository) Create(
	ctx context.Context,
	arg CreateSAMLIdentityParams,
) (gen.UserSamlIdentity, error) {
	raw, err := encodeAttributes(arg.Attributes)
	if err != nil {
		return gen.UserSamlIdentity{}, err
	}
	return r.q.CreateSAMLIdentity(ctx, gen.CreateSAMLIdentityParams{
		UserID:         arg.UserID,
		Provider:       arg.Provider,
		NameID:         arg.NameID,
		IdpEntityID:    arg.IdpEntityID,
		AttributesJson: raw,
	})
}

// Get returns the identity row by id.
func (r *SamlIdentitiesRepository) Get(ctx context.Context, id uuid.UUID) (gen.UserSamlIdentity, error) {
	return r.q.GetSAMLIdentity(ctx, id)
}

// GetByProviderNameID returns the identity row for the given (provider, name_id)
// pair — the ACS path lookup after the IdP posts back a signed assertion.
func (r *SamlIdentitiesRepository) GetByProviderNameID(
	ctx context.Context,
	provider, nameID string,
) (gen.UserSamlIdentity, error) {
	return r.q.GetSAMLIdentityByProviderNameID(ctx, gen.GetSAMLIdentityByProviderNameIDParams{
		Provider: provider,
		NameID:   nameID,
	})
}

// GetForUser returns the identity row for the given (userID, provider) pair —
// the link/unlink path lookup.
func (r *SamlIdentitiesRepository) GetForUser(
	ctx context.Context,
	userID uuid.UUID,
	provider string,
) (gen.UserSamlIdentity, error) {
	return r.q.GetSAMLIdentityForUser(ctx, gen.GetSAMLIdentityForUserParams{
		UserID:   userID,
		Provider: provider,
	})
}

// ListForUser returns every SAML identity linked to the user, newest first.
func (r *SamlIdentitiesRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.UserSamlIdentity, error) {
	return r.q.ListSAMLIdentitiesForUser(ctx, userID)
}

// UpdateSAMLIdentityAttributesParams rotates the attribute snapshot + IdP
// entity id on the given identity row. Called on every successful ACS so the
// user's profile reflects the latest claims the IdP asserted.
type UpdateSAMLIdentityAttributesParams struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Attributes  map[string]any
	IdpEntityID string
}

// UpdateAttributes refreshes the stored attribute snapshot.
func (r *SamlIdentitiesRepository) UpdateAttributes(
	ctx context.Context,
	arg UpdateSAMLIdentityAttributesParams,
) error {
	raw, err := encodeAttributes(arg.Attributes)
	if err != nil {
		return err
	}
	return r.q.UpdateSAMLIdentityAttributes(ctx, gen.UpdateSAMLIdentityAttributesParams{
		ID:             arg.ID,
		UserID:         arg.UserID,
		AttributesJson: raw,
		IdpEntityID:    arg.IdpEntityID,
	})
}

// Delete removes the identity row. The "at least one auth method remaining"
// invariant is enforced in the service layer (which counts password_hash +
// OAuth/OIDC identities + other SAML identities first); this repo just deletes.
func (r *SamlIdentitiesRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return r.q.DeleteSAMLIdentity(ctx, gen.DeleteSAMLIdentityParams{ID: id, UserID: userID})
}

// CountForUser returns the number of SAML identities linked to the user. Used
// by the unlink path to enforce "at least one auth method remaining".
func (r *SamlIdentitiesRepository) CountForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return r.q.CountSAMLIdentitiesForUser(ctx, userID)
}

// encodeAttributes marshals the attribute map to a JSONB-friendly RawMessage.
// A nil map is encoded as "{}" so the column never carries NULL (the schema's
// DEFAULT '{}'::jsonb would not fire on UPDATE, which the service uses to
// refresh attributes).
func encodeAttributes(attrs map[string]any) (json.RawMessage, error) {
	if attrs == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(attrs)
	if err != nil {
		// Wrapping at the repo boundary (rather than letting the caller see a
		// raw json.Marshal error) keeps the error trail consistent with the
		// other repos that wrap pgx errors with "<area>:" prefixes.
		return nil, fmt.Errorf("saml_identities: encode attributes: %w", err)
	}
	return raw, nil
}
