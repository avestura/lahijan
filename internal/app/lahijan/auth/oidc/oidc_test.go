// oidc_test.go covers the OIDC provider surface against an in-process fake
// IdP (auth/oidc/fake): discovery, BuildAuthURL with PKCE + nonce, Exchange
// with id_token verification (signature + audience + nonce), and the
// registry. The fake signs id_tokens with a real RSA key so Lahijan's full
// verification path is exercised end to end.
package oidc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
)

// noopVerifier lets the OIDC tests stay focused on OIDC plumbing (state
// verification has its own coverage in auth/state).
func noopVerifier(_, _, _, _ string) error { return nil }

// realVerifier wires up the actual auth/state.Signer so BuildAuthURL + the
// callback's VerifyState round-trip through the real signing path.
func realVerifier(t *testing.T) func(string, string, string, string) error {
	t.Helper()
	return state.NewSigner(secrets.NewSigner("oidc-test-signing-key")).Verify
}

// newFakeProvider spins up the in-process OIDC fake and binds an OIDC
// provider to it. Closes the fake on test cleanup.
func newFakeProvider(t *testing.T, verifier func(string, string, string, string) error) (*fake.Server, oidc.Provider) {
	t.Helper()
	srv := fake.New()
	t.Cleanup(srv.Close)

	p, err := oidc.NewProvider(context.Background(), oidc.ProviderConfig{
		Key:          "test-idp",
		Issuer:       srv.Issuer,
		ClientID:     "oidc-client-id",
		ClientSecret: "oidc-client-secret",
		RedirectURL:  "https://app.test/api/v1/auth/oidc/test-idp/callback",
		Scopes:       []string{"openid", "email", "profile"},
	}, verifier)
	require.NoError(t, err, "discovery must succeed against the fake")
	return srv, p
}

func TestProvider_DiscoverySucceeds(t *testing.T) {
	t.Parallel()
	_, p := newFakeProvider(t, noopVerifier)
	assert.Equal(t, "test-idp", p.Key())
}

func TestProvider_DiscoveryFailure(t *testing.T) {
	t.Parallel()
	// A bogus issuer URL yields a discovery error wrapped in ErrDiscovery.
	_, err := oidc.NewProvider(context.Background(), oidc.ProviderConfig{
		Key:    "broken",
		Issuer: "http://127.0.0.1:0/not-a-real-issuer",
	}, noopVerifier)
	require.Error(t, err)
	assert.ErrorIs(t, err, oidc.ErrDiscovery)
}

func TestProvider_BuildAuthURL_HasOIDCAndPKCEParams(t *testing.T) {
	t.Parallel()
	_, p := newFakeProvider(t, noopVerifier)

	url, verifier, nonce, err := p.BuildAuthURL("STATE")
	require.NoError(t, err)

	assert.Contains(t, url, "response_type=code")
	assert.Contains(t, url, "client_id=oidc-client-id")
	assert.Contains(t, url, "state=STATE")
	assert.Contains(t, url, "scope=openid")    // OIDC scope
	assert.Contains(t, url, "code_challenge=") // PKCE challenge
	assert.Contains(t, url, "code_challenge_method=S256")
	assert.Contains(t, url, "nonce=") // OIDC nonce param
	assert.NotEmpty(t, nonce)
	assert.NotContains(t, url, verifier) // verifier must NOT be on the URL
	assert.GreaterOrEqual(t, len(verifier), 43)
	assert.GreaterOrEqual(t, len(nonce), 43)
}

func TestProvider_Exchange_VerifiesIDToken(t *testing.T) {
	t.Parallel()
	srv, p := newFakeProvider(t, noopVerifier)

	authURL, verifier, nonce, err := p.BuildAuthURL("STATE")
	require.NoError(t, err)
	require.NotEmpty(t, nonce)

	// Hit /auth so the fake captures the nonce before Exchange.
	_, _ = srv.HTTP.Client().Get(authURL) //nolint:bodyclose // we don't need the body

	tok, err := p.Exchange(context.Background(), srv.AuthCode, verifier, nonce)
	require.NoError(t, err, "id_token verification must succeed for a token signed by the fake's JWKS key")
	assert.Equal(t, "fake-oidc-access-token", tok.AccessToken)
	assert.Equal(t, "fake-oidc-refresh-token", tok.RefreshToken)
	assert.NotEmpty(t, tok.IDToken, "IDToken must be carried through")
	assert.Equal(t, "oidc-sub-42", tok.Claims.Subject)
	assert.Equal(t, "oidc-user@example.test", tok.Claims.Email)
	assert.True(t, tok.Claims.EmailVerified)
	assert.Equal(t, "OIDC User", tok.Claims.Name)
	assert.Equal(t, nonce, tok.Claims.Nonce, "the id_token nonce must echo the one we sent")
	assert.Equal(t, 1, srv.ExchangeCount(), "the /token endpoint must have been hit exactly once")
}

func TestProvider_Exchange_RejectsBadNonce(t *testing.T) {
	t.Parallel()
	srv, p := newFakeProvider(t, noopVerifier)

	authURL, verifier, _, err := p.BuildAuthURL("STATE")
	require.NoError(t, err)
	_, _ = srv.HTTP.Client().Get(authURL) //nolint:bodyclose // capture the nonce in the fake

	// Exchange with a deliberately wrong nonce; the id_token's nonce will
	// not match what we presented.
	_, err = p.Exchange(context.Background(), srv.AuthCode, verifier, "wrong-nonce")
	require.Error(t, err, "nonce mismatch must reject")
	assert.ErrorIs(t, err, oidc.ErrIDToken)
}

func TestProvider_Exchange_BogusCodeRejected(t *testing.T) {
	t.Parallel()
	_, p := newFakeProvider(t, noopVerifier)

	_, verifier, nonce, err := p.BuildAuthURL("STATE")
	require.NoError(t, err)
	_, err = p.Exchange(context.Background(), "definitely-not-issued", verifier, nonce)
	require.Error(t, err, "an unissued code must fail the exchange")
}

func TestProvider_StateVerifierIsWired(t *testing.T) {
	t.Parallel()
	_, p := newFakeProvider(t, realVerifier(t))

	s := state.NewSigner(secrets.NewSigner("oidc-test-signing-key"))
	token, nonce, err := s.Issue("test-idp", "")
	require.NoError(t, err)

	require.NoError(t, p.VerifyState(token, nonce, ""))
	err = p.VerifyState(token, "wrong-nonce", "")
	assert.ErrorIs(t, err, state.ErrInvalid)
}

func TestRegistry_Lookup(t *testing.T) {
	t.Parallel()
	srv := fake.New()
	t.Cleanup(srv.Close)
	p1, err := oidc.NewProvider(context.Background(), oidc.ProviderConfig{
		Key: "alpha", Issuer: srv.Issuer, ClientID: "x", ClientSecret: "y", RedirectURL: "https://app.test/cb",
	}, noopVerifier)
	require.NoError(t, err)
	p2, err := oidc.NewProvider(context.Background(), oidc.ProviderConfig{
		Key: "beta", Issuer: srv.Issuer, ClientID: "x", ClientSecret: "y", RedirectURL: "https://app.test/cb",
	}, noopVerifier)
	require.NoError(t, err)

	reg := oidc.NewRegistry(p1, p2)
	got, err := reg.Lookup("alpha")
	require.NoError(t, err)
	assert.Equal(t, p1, got)

	got2, err := reg.Lookup("beta")
	require.NoError(t, err)
	assert.Equal(t, p2, got2)

	_, err = reg.Lookup("unknown")
	assert.ErrorIs(t, err, oidc.ErrProviderUnknown)
}
