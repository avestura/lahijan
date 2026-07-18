// Package idp: registry.go is the unified lookup for external identity
// providers. It composes the OAuth and OIDC registries into a single
// ProviderLookup the api handler can call without caring which layer a given
// provider lives in.
//
// The OAuth providers are keyed by their stable name ("google", "github");
// the OIDC providers are keyed by "oidc:<config-key>" so they never collide
// with OAuth presets. The "oidc:" prefix matches the OIDCAdapter.Key value
// the service stores on user_oauth_identities.provider, so a callback can be
// routed back to the right adapter by reading the path param.
package idp

import (
	"fmt"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
)

// CombinedRegistry is a ProviderLookup that searches the OAuth registry
// first, then the OIDC registry. The OIDC lookup is keyed by the raw config
// key; the registry adds the "oidc:" prefix internally so callers do not
// need to.
type CombinedRegistry struct {
	oauth *oauth.Registry
	oidc  *oidc.Registry
}

// NewCombinedRegistry builds the unified lookup. Either argument may be nil
// to disable that layer entirely (e.g. an OAuth-only deployment).
func NewCombinedRegistry(oauthReg *oauth.Registry, oidcReg *oidc.Registry) *CombinedRegistry {
	return &CombinedRegistry{oauth: oauthReg, oidc: oidcReg}
}

// Lookup resolves the provider key to an ExternalIDP.
//
// For an OAuth provider key ("google", "github", ...), Lookup returns an
// OAuthAdapter wrapping the matching oauth.Provider.
//
// For an OIDC provider key ("oidc:keycloak", "oidc:auth0", ...), Lookup
// returns an OIDCAdapter wrapping the matching oidc.Provider. The leading
// "oidc:" prefix is stripped before looking up in the OIDC registry.
//
// Keys that start with neither pattern (or that have no matching provider in
// their registry) return a wrapped ErrProviderUnknown.
func (r *CombinedRegistry) Lookup(key string) (ExternalIDP, error) {
	// OAuth preset keys are exactly what's in the OAuth registry.
	if r.oauth != nil {
		if p, err := r.oauth.Lookup(key); err == nil {
			return &OAuthAdapter{P: p}, nil
		}
	}
	// OIDC keys carry the "oidc:" prefix (matches OIDCAdapter.Key).
	if r.oidc != nil && len(key) > len("oidc:") && key[:len("oidc:")] == "oidc:" {
		inner := key[len("oidc:"):]
		if p, err := r.oidc.Lookup(inner); err == nil {
			return &OIDCAdapter{P: p}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrProviderUnknown, key)
}

// ErrProviderUnknown is returned by Lookup when the key matches no configured
// provider in either registry. Wraps oauth.ErrProviderUnknown so the api
// handler can errors.Is() either.
var ErrProviderUnknown = oauth.ErrProviderUnknown
