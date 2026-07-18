// Package webauthn implements Lahijan's WebAuthn relying-party (RP) surface
// (WS-07c): the half of the W3C WebAuthn ceremony that lives on the server.
// The package exposes two ceremony pairs:
//
//   - Registration (attestation):
//       BeginRegistration(user) -> (CredentialCreation, SessionData)
//       FinishRegistration(user, SessionData, parsed) -> Credential
//   - Authentication (assertion):
//       BeginLogin(user) -> (CredentialAssertion, SessionData)
//       FinishLogin(user, SessionData, parsed) -> Credential
//
// The package wraps `github.com/go-webauthn/webauthn` (Apache-2.0; see
// ADR-0021) so callers in auth/mfa never import the upstream library
// directly. The wrapping keeps the WS-07c surface replaceable.
//
// PERSISTENCE: the package does NOT touch the database. The SessionData
// returned from Begin* must be persisted by the caller (typically in
// mfa_pending_sessions or a short-lived cache row) and handed back to the
// matching Finish*. The package exposes Marshal/UnmarshalSession helpers
// that turn SessionData into a JSON []byte for storage.
package webauthn

import (
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Config carries the deployer-controlled WebAuthn relying-party fields.
// RPID is the domain the browser is on (e.g. "app.example.com"), without
// scheme or port. RPOrigins is the full list of origins the RP accepts
// (e.g. "https://app.example.com"). RPID + at least one RPOrigins entry
// are required; an empty RPID at construction returns ErrConfig.
type Config struct {
	// RPID is the WebAuthn relying-party id (the registrable domain).
	RPID string
	// RPDisplayName is the human-friendly name shown in the browser
	// prompt ("Lahijan" by default).
	RPDisplayName string
	// RPOrigins is the list of permitted origins (full scheme + host
	// + optional port). Must be non-empty.
	RPOrigins []string
	// RPTopOrigins is the optional list of permitted top origins for
	// Level 3 cross-iframe flows. Leave empty to disable top-origin
	// verification (the Level 2 default).
	RPTopOrigins []string
}

// DefaultConfig returns a config that matches the WS-07c scope. Callers
// MUST override RPID and RPOrigins from conf.auth.mfa.webauthn.* — the
// defaults are intentionally empty so a misconfigured RP fails closed at
// construction rather than silently issuing credentials that browsers
// reject.
func DefaultConfig() Config {
	return Config{
		RPDisplayName: "Lahijan",
	}
}

// ErrConfig is returned by New when the supplied Config is incomplete
// (missing RPID, missing RPOrigins, or RPID does not match any origin).
var ErrConfig = errors.New("webauthn: incomplete relying-party config")

// RelyingParty is the WebAuthn RP surface the MFA service talks to. The
// shape mirrors go-webauthn's *WebAuthn so the wrapping is mechanical.
// Tests pass a fake; production passes a concrete *RP built by New.
type RelyingParty interface {
	BeginRegistration(user User) (creation *CredentialCreation, session SessionData, err error)
	FinishRegistration(user User, session SessionData, parsed *ParsedCreation) (*Credential, error)
	BeginLogin(user User) (assertion *CredentialAssertion, session SessionData, err error)
	FinishLogin(user User, session SessionData, parsed *ParsedAssertion) (*Credential, error)
}

// User is the WebAuthn user abstraction the MFA service satisfies. It
// mirrors go-webauthn/webauthn.User so the wrapping is mechanical; the
// package-level adapter in user.go converts between this surface and the
// upstream one.
//
// WebAuthnID MUST be at most 64 bytes (the W3C spec hard limit); the
// adapter rejects longer values.
type User interface {
	WebAuthnID() []byte
	WebAuthnName() string
	WebAuthnDisplayName() string
	WebAuthnCredentials() []Credential
}

// Credential is the RP-side representation of a WebAuthn credential: the
// credential id (raw bytes), the COSE public key, the sign counter, the
// transports list, and the attestation format. The MFA service persists
// this in user_webauthn_credentials.
type Credential struct {
	// ID is the raw credential id bytes from the authenticator. The MFA
	// service persists it as base64url in
	// user_webauthn_credentials.credential_id.
	ID []byte
	// PublicKey is the COSE-encoded public key. The MFA service persists
	// it raw in user_webauthn_credentials.public_key.
	PublicKey []byte
	// AttestationType is the attestation format the authenticator used
	// ("none", "packed", "tpm", "android-key", ...).
	AttestationType string
	// Transport is the list of UI hints (usb, nfc, ble, internal, ...).
	Transport []string
	// SignCount is the WebAuthn replay-detection counter.
	SignCount uint32
}

// CredentialCreation mirrors protocol.CredentialCreation: the JSON-shape
// the browser hands to navigator.credentials.create(). The api handler
// JSON-encodes this verbatim.
type CredentialCreation struct {
	// Response is the inner PublicKeyCredentialCreationOptions struct.
	// It is opaque to Lahijan — we never modify it; we just round-trip
	// it to/from the browser.
	Response any `json:"publicKey"`
}

// CredentialAssertion mirrors protocol.CredentialAssertion: the JSON-shape
// the browser hands to navigator.credentials.get(). Same opacity
// contract as CredentialCreation.Response.
type CredentialAssertion struct {
	Response any `json:"publicKey"`
}

// ParsedCreation carries the parsed creation response the browser POSTs
// back after navigator.credentials.create() succeeds. The api handler
// runs protocol.ParseCredentialCreationResponseBody on the request body
// and passes the resulting *ParsedCredentialCreationData here.
type ParsedCreation struct {
	inner *protocol.ParsedCredentialCreationData
}

// ParsedAssertion carries the parsed assertion response the browser POSTs
// back after navigator.credentials.get() succeeds. Mirrors ParsedCreation
// for the login side.
type ParsedAssertion struct {
	inner *protocol.ParsedCredentialAssertionData
}

// RP is the concrete WebAuthn relying-party. Build one with New and share
// it across requests.
type RP struct {
	inner *webauthn.WebAuthn
}

// New builds a WebAuthn relying-party from the supplied config. Returns
// ErrConfig when the config is incomplete (missing RPID, missing origins,
// or RPID does not match any origin).
func New(c Config) (*RP, error) {
	if c.RPID == "" {
		return nil, fmt.Errorf("%w: RPID is required", ErrConfig)
	}
	if len(c.RPOrigins) == 0 {
		return nil, fmt.Errorf("%w: at least one RPOrigins entry is required", ErrConfig)
	}
	display := c.RPDisplayName
	if display == "" {
		display = "Lahijan"
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          c.RPID,
		RPDisplayName: display,
		RPOrigins:     c.RPOrigins,
		RPTopOrigins:  c.RPTopOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn: build RP: %w", err)
	}
	return &RP{inner: w}, nil
}

// BeginRegistration starts the registration ceremony: mints a fresh
// challenge + session, returns the JSON the browser needs to call
// navigator.credentials.create(), and the session the RP needs to
// remember between this call and FinishRegistration.
func (r *RP) BeginRegistration(user User) (*CredentialCreation, SessionData, error) {
	wuser := newUserAdapter(user)
	creation, sess, err := r.inner.BeginRegistration(wuser)
	if err != nil {
		return nil, SessionData{}, fmt.Errorf("webauthn: begin registration: %w", err)
	}
	out, err := MarshalSession(sess)
	if err != nil {
		return nil, SessionData{}, fmt.Errorf("webauthn: marshal registration session: %w", err)
	}
	return &CredentialCreation{Response: creation.Response}, out, nil
}

// FinishRegistration completes the registration ceremony: verifies the
// attestation signature, extracts the public key, and returns the new
// Credential the MFA service persists.
func (r *RP) FinishRegistration(user User, session SessionData, parsed *ParsedCreation) (*Credential, error) {
	if parsed == nil || parsed.inner == nil {
		return nil, errors.New("webauthn: parsed creation is nil")
	}
	wuser := newUserAdapter(user)
	sess, err := UnmarshalSession(session)
	if err != nil {
		return nil, fmt.Errorf("webauthn: unmarshal registration session: %w", err)
	}
	cred, err := r.inner.CreateCredential(wuser, *sess, parsed.inner)
	if err != nil {
		return nil, fmt.Errorf("webauthn: finish registration: %w", err)
	}
	return adaptCredentialBack(cred), nil
}

// BeginLogin starts the authentication ceremony: mints a fresh challenge
// + session, returns the JSON the browser needs to call
// navigator.credentials.get(), and the session the RP needs to remember
// between this call and FinishLogin.
func (r *RP) BeginLogin(user User) (*CredentialAssertion, SessionData, error) {
	wuser := newUserAdapter(user)
	assertion, sess, err := r.inner.BeginLogin(wuser)
	if err != nil {
		return nil, SessionData{}, fmt.Errorf("webauthn: begin login: %w", err)
	}
	out, err := MarshalSession(sess)
	if err != nil {
		return nil, SessionData{}, fmt.Errorf("webauthn: marshal login session: %w", err)
	}
	return &CredentialAssertion{Response: assertion.Response}, out, nil
}

// FinishLogin completes the authentication ceremony: verifies the
// assertion signature, bumps the sign counter, and returns the
// (now-validated) Credential the MFA service updates the stored sign_count for.
func (r *RP) FinishLogin(user User, session SessionData, parsed *ParsedAssertion) (*Credential, error) {
	if parsed == nil || parsed.inner == nil {
		return nil, errors.New("webauthn: parsed assertion is nil")
	}
	wuser := newUserAdapter(user)
	sess, err := UnmarshalSession(session)
	if err != nil {
		return nil, fmt.Errorf("webauthn: unmarshal login session: %w", err)
	}
	cred, err := r.inner.ValidateLogin(wuser, *sess, parsed.inner)
	if err != nil {
		return nil, fmt.Errorf("webauthn: finish login: %w", err)
	}
	return adaptCredentialBack(cred), nil
}

// ParseCreationResponseBody runs protocol.ParseCredentialCreationResponseBody
// on the raw HTTP request body the browser POSTed at
// /api/v1/me/mfa/webauthn/register/finish. The api handler calls this
// and passes the result to FinishRegistration.
//
// The signature takes []byte (not io.Reader) so the api handler can buffer
// the body once and reuse it for logging / debugging.
func ParseCreationResponseBody(body []byte) (*ParsedCreation, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return nil, fmt.Errorf("webauthn: parse creation response: %w", err)
	}
	return &ParsedCreation{inner: parsed}, nil
}

// ParseAssertionResponseBody runs
// protocol.ParseCredentialRequestResponseBody on the raw HTTP request body
// the browser POSTed at /api/v1/me/mfa/webauthn/login/finish.
func ParseAssertionResponseBody(body []byte) (*ParsedAssertion, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return nil, fmt.Errorf("webauthn: parse assertion response: %w", err)
	}
	return &ParsedAssertion{inner: parsed}, nil
}

// adaptCredentialBack converts the upstream library's Credential pointer
// into the package's own Credential shape.
func adaptCredentialBack(c *webauthn.Credential) *Credential {
	if c == nil {
		return nil
	}
	return &Credential{
		ID:              c.ID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       adaptTransportsBack(c.Transport),
		SignCount:       c.Authenticator.SignCount,
	}
}

// adaptTransports converts the package's []string transport list into the
// upstream library's []protocol.AuthenticatorTransport.
func adaptTransports(t []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, 0, len(t))
	for _, s := range t {
		out = append(out, protocol.AuthenticatorTransport(s))
	}
	return out
}

// adaptTransportsBack is the inverse of adaptTransports.
func adaptTransportsBack(t []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(t))
	for _, x := range t {
		out = append(out, string(x))
	}
	return out
}
