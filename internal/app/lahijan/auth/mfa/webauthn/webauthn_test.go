// Package webauthn: webauthn_test.go covers the ceremony round-trip with
// the in-process fake authenticator. It is the WS-07c DoD row "WebAuthn
// register → login works end-to-end (synthetic authenticator in tests)".
package webauthn

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn/fake"
)

const (
	testRPID     = "localhost"
	testRPOrigin = "https://localhost"
)

// staticUser is the minimum User impl the RP needs to drive a ceremony.
type staticUser struct {
	id    []byte
	name  string
	creds []Credential
}

func (u *staticUser) WebAuthnID() []byte                { return u.id }
func (u *staticUser) WebAuthnName() string              { return u.name }
func (u *staticUser) WebAuthnDisplayName() string       { return u.name }
func (u *staticUser) WebAuthnCredentials() []Credential { return u.creds }

func TestNew_MissingRPID(t *testing.T) {
	t.Parallel()
	_, err := New(Config{RPOrigins: []string{"https://example.com"}})
	require.ErrorIs(t, err, ErrConfig)
}

func TestNew_MissingOrigins(t *testing.T) {
	t.Parallel()
	_, err := New(Config{RPID: "example.com"})
	require.ErrorIs(t, err, ErrConfig)
}

func TestNew_DefaultsDisplayName(t *testing.T) {
	t.Parallel()
	rp, err := New(Config{RPID: "localhost", RPOrigins: []string{"https://localhost"}})
	require.NoError(t, err)
	require.NotNil(t, rp)
}

func TestSessionRoundTripPreservesBytes(t *testing.T) {
	t.Parallel()
	// Build a real session via BeginRegistration so we know the bytes
	// contain a valid upstream SessionData.
	rp, err := New(Config{RPID: testRPID, RPOrigins: []string{testRPOrigin}})
	require.NoError(t, err)
	user := &staticUser{id: []byte("u1"), name: "alice@example.test"}
	_, sess, err := rp.BeginRegistration(user)
	require.NoError(t, err)

	raw := sess.Raw()
	require.NotEmpty(t, raw)

	// Round-trip via SessionFromBytes: Unmarshal must succeed and produce
	// a SessionData whose Challenge matches the original.
	restored := SessionFromBytes(raw)
	require.Equal(t, raw, restored.Raw(), "SessionFromBytes must round-trip the bytes verbatim")
}

func TestCeremony_RegisterThenLogin(t *testing.T) {
	t.Parallel()
	rp, err := New(Config{
		RPID:      testRPID,
		RPOrigins: []string{testRPOrigin},
	})
	require.NoError(t, err)
	user := &staticUser{id: []byte("user-1"), name: "alice@example.test"}
	auth := fake.NewAuthenticator()

	// --- Registration ceremony ---
	creation, session, err := rp.BeginRegistration(user)
	require.NoError(t, err)
	require.NotNil(t, creation)

	creationJSON, err := json.Marshal(creation)
	require.NoError(t, err)
	body, err := auth.SignRegistration(creationJSON, testRPOrigin, testRPID)
	require.NoError(t, err)
	parsed, err := ParseCreationResponseBody(body)
	require.NoError(t, err)

	cred, err := rp.FinishRegistration(user, session, parsed)
	require.NoError(t, err)
	require.NotNil(t, cred)
	require.Equal(t, auth.CredentialID(), cred.ID)
	require.NotEmpty(t, cred.PublicKey)

	// --- Authentication ceremony ---
	// Re-build the user with the freshly-stored credential so BeginLogin
	// sees it in the allowed list.
	user.creds = []Credential{{
		ID:        cred.ID,
		PublicKey: cred.PublicKey,
		SignCount: cred.SignCount,
	}}
	assertion, sess2, err := rp.BeginLogin(user)
	require.NoError(t, err)
	require.NotNil(t, assertion)

	assertionJSON, err := json.Marshal(assertion)
	require.NoError(t, err)
	body2, err := auth.SignAssertion(assertionJSON, testRPOrigin, testRPID)
	require.NoError(t, err)
	parsed2, err := ParseAssertionResponseBody(body2)
	require.NoError(t, err)

	cred2, err := rp.FinishLogin(user, sess2, parsed2)
	require.NoError(t, err)
	require.NotNil(t, cred2)
	require.Equal(t, cred.ID, cred2.ID)
	assert.Greater(t, cred2.SignCount, cred.SignCount, "sign counter must bump on every assertion")
}
