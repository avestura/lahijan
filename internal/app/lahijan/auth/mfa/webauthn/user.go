// Package webauthn: user.go adapts the package's own User + Credential
// shapes into the upstream library's webauthn.User + webauthn.Credential
// shapes. The adapter lives here so callers never import the upstream
// library directly.
package webauthn

import (
	"github.com/go-webauthn/webauthn/webauthn"
)

// userAdapter wraps the package's User interface and satisfies the
// upstream library's webauthn.User. The credentials are pre-converted at
// construction so WebAuthnCredentials() is a pure getter (the upstream
// interface expects a []Credential and not a conversion per call).
type userAdapter struct {
	u     User
	creds []webauthn.Credential
}

// newUserAdapter builds the adapter and pre-converts the credentials.
func newUserAdapter(u User) *userAdapter {
	creds := make([]webauthn.Credential, 0, len(u.WebAuthnCredentials()))
	for _, c := range u.WebAuthnCredentials() {
		creds = append(creds, webauthn.Credential{
			ID:              c.ID,
			PublicKey:       c.PublicKey,
			AttestationType: c.AttestationType,
			Transport:       adaptTransports(c.Transport),
			Authenticator: webauthn.Authenticator{
				SignCount: c.SignCount,
			},
		})
	}
	return &userAdapter{u: u, creds: creds}
}

// WebAuthnID implements webauthn.User.
func (a *userAdapter) WebAuthnID() []byte { return a.u.WebAuthnID() }

// WebAuthnName implements webauthn.User.
func (a *userAdapter) WebAuthnName() string { return a.u.WebAuthnName() }

// WebAuthnDisplayName implements webauthn.User.
func (a *userAdapter) WebAuthnDisplayName() string { return a.u.WebAuthnDisplayName() }

// WebAuthnCredentials implements webauthn.User.
func (a *userAdapter) WebAuthnCredentials() []webauthn.Credential { return a.creds }
