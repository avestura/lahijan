// Package fake provides an in-process synthetic WebAuthn authenticator for
// integration tests (WS-07c). It signs real attestation + assertion objects
// the relying-party can verify, without requiring a browser, USB key, or
// platform authenticator.
//
// The fake uses ES256 (ECDSA P-256 + SHA-256) — the most widely supported
// WebAuthn algorithm — and the "none" attestation format (the RP does not
// verify the authenticator's make/model; it accepts any key). The output
// bytes are real CBOR + real signatures, so the go-webauthn library's
// verification path is exercised end to end.
//
// Test usage:
//
//	auth := fake.NewAuthenticator()
//	creation, sess, _ := rp.BeginRegistration(user)
//	body := auth.SignRegistration(creation, "https://app.example.com", "example.com")
//	parsed, _ := webauthn.ParseCreationResponseBody(body)
//	cred, _ := rp.FinishRegistration(user, sess, parsed)
package fake

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator is a synthetic WebAuthn authenticator. Build one with New
// and call SignRegistration / SignAssertion to produce the JSON bytes the
// api handler would normally receive from the browser.
type Authenticator struct {
	// priv is the ECDSA P-256 private key the fake signs with.
	priv *ecdsa.PrivateKey
	// credID is the credential id the fake advertises at registration
	// time. Saved so SignAssertion can sign with the same key + credID.
	credID []byte
	// counter is the WebAuthn sign counter; bumped on every assertion.
	counter uint32
}

// NewAuthenticator builds a fake with a fresh ECDSA P-256 keypair and a
// 16-byte random credential id. The same key + credID are reused across
// SignRegistration + every SignAssertion for this instance, mirroring how
// a real authenticator persists one key per credential.
func NewAuthenticator() *Authenticator {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		// ecdsa.GenerateKey only fails when rand.Reader fails, which is
		// fatal in any test setup.
		panic(fmt.Sprintf("fake: generate ecdsa key: %v", err))
	}
	credID := make([]byte, 16)
	if _, err := rand.Read(credID); err != nil {
		panic(fmt.Sprintf("fake: read credential id: %v", err))
	}
	return &Authenticator{priv: priv, credID: credID}
}

// PublicKey returns the credential's public key. Useful for tests that
// want to assert the stored value matches what the authenticator signed with.
func (a *Authenticator) PublicKey() *ecdsa.PublicKey { return &a.priv.PublicKey }

// CredentialID returns the credential id as a copy so callers cannot
// mutate the fake's internal state.
func (a *Authenticator) CredentialID() []byte {
	out := make([]byte, len(a.credID))
	copy(out, a.credID)
	return out
}

// SignRegistration builds the JSON bytes a browser would POST to
// /webauthn/register/finish after navigator.credentials.create() succeeds.
// Pass the JSON the RP returned from BeginRegistration verbatim — the
// fake extracts the challenge from it.
//
// rpOrigin is the full origin (e.g. "https://app.example.com").
// rpID is the registrable domain (e.g. "app.example.com").
func (a *Authenticator) SignRegistration(creationJSON []byte, rpOrigin, rpID string) ([]byte, error) {
	challenge, err := extractChallenge(creationJSON, "webauthn.create")
	if err != nil {
		return nil, err
	}
	clientData := clientDataJSON{
		Type:      "webauthn.create",
		Challenge: challenge,
		Origin:    rpOrigin,
	}
	clientDataRaw, err := json.Marshal(clientData)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal client data: %w", err)
	}

	coseKey := buildCOSEKey(a.priv.PublicKey)
	coseKeyCBOR, err := cbor.Marshal(coseKey)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal cose key: %w", err)
	}

	authData, err := buildAuthData(rpID, true, a.counter, &attestedCredentialData{
		AAGUID:               make([]byte, 16),
		CredentialID:         a.credID,
		CredentialPublicKey:  coseKeyCBOR,
	})
	if err != nil {
		return nil, err
	}

	attObj := map[string]any{
		"fmt":     "none",
		"attStmt": map[string]any{},
		"authData": authData,
	}
	attObjCBOR, err := cbor.Marshal(attObj)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal attestation object: %w", err)
	}

	resp := registrationResponse{
		ID:      base64.RawURLEncoding.EncodeToString(a.credID),
		RawID:   base64.RawURLEncoding.EncodeToString(a.credID),
		Type:    "public-key",
		Response: registrationResponseInner{
			AttestationObject: base64.RawURLEncoding.EncodeToString(attObjCBOR),
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRaw),
		},
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal registration response: %w", err)
	}
	return out, nil
}

// SignAssertion builds the JSON bytes a browser would POST to
// /webauthn/login/finish after navigator.credentials.get() succeeds.
// rpOrigin + rpID match SignRegistration.
func (a *Authenticator) SignAssertion(assertionJSON []byte, rpOrigin, rpID string) ([]byte, error) {
	// Bump the sign counter on every assertion — the RP rejects any
	// future assertion whose counter is not strictly greater.
	a.counter++

	challenge, err := extractChallenge(assertionJSON, "webauthn.get")
	if err != nil {
		return nil, err
	}
	clientData := clientDataJSON{
		Type:      "webauthn.get",
		Challenge: challenge,
		Origin:    rpOrigin,
	}
	clientDataRaw, err := json.Marshal(clientData)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal client data: %w", err)
	}

	authData, err := buildAuthData(rpID, true, a.counter, nil)
	if err != nil {
		return nil, err
	}

	clientDataHash := sha256.Sum256(clientDataRaw)
	signed := append(authData, clientDataHash[:]...)
	// ECDSA always signs a digest. The library verifies by SHA-256'ing
	// (authData || SHA256(clientDataJSON)) and asking ecdsa.Verify to
	// match R/S against that digest, so we sign the same digest here.
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, a.priv, digest[:])
	if err != nil {
		return nil, fmt.Errorf("fake: sign assertion: %w", err)
	}

	resp := assertionResponse{
		ID:    base64.RawURLEncoding.EncodeToString(a.credID),
		RawID: base64.RawURLEncoding.EncodeToString(a.credID),
		Type:  "public-key",
		Response: assertionResponseInner{
			AuthenticatorData: base64.RawURLEncoding.EncodeToString(authData),
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRaw),
			Signature:         base64.RawURLEncoding.EncodeToString(sig),
		},
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("fake: marshal assertion response: %w", err)
	}
	return out, nil
}

// clientDataJSON is the W3C WebAuthn CollectedClientData shape.
type clientDataJSON struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
	// CrossOrigin is set to false to mirror what real browsers send when
	// the request originates from the RP's own origin.
	CrossOrigin bool `json:"crossOrigin"`
}

// registrationResponse is the JSON shape the browser POSTs at /register/finish.
type registrationResponse struct {
	ID       string                  `json:"id"`
	RawID    string                  `json:"rawId"`
	Type     string                  `json:"type"`
	Response registrationResponseInner `json:"response"`
}

type registrationResponseInner struct {
	AttestationObject string `json:"attestationObject"`
	ClientDataJSON    string `json:"clientDataJSON"`
}

// assertionResponse is the JSON shape the browser POSTs at /login/finish.
type assertionResponse struct {
	ID       string                 `json:"id"`
	RawID    string                 `json:"rawId"`
	Type     string                 `json:"type"`
	Response assertionResponseInner `json:"response"`
}

type assertionResponseInner struct {
	AuthenticatorData string `json:"authenticatorData"`
	ClientDataJSON    string `json:"clientDataJSON"`
	Signature         string `json:"signature"`
}

// attestedCredentialData is the W3C attested credential data block.
type attestedCredentialData struct {
	AAGUID              []byte
	CredentialID        []byte
	CredentialPublicKey []byte
}

// buildAuthData constructs the WebAuthn authenticatorData byte sequence.
// When includeAttested is true, the AT flag is set and the attested
// credential data is appended (registration case). Otherwise the data is
// just rpIdHash + flags + signCount (assertion case).
func buildAuthData(rpID string, userPresent bool, counter uint32, att *attestedCredentialData) ([]byte, error) {
	rpIDHash := sha256.Sum256([]byte(rpID))
	var flags byte
	if userPresent {
		flags |= 0x01 // UP
	}
	if att != nil {
		flags |= 0x40 // AT
	}
	out := make([]byte, 0, 37)
	out = append(out, rpIDHash[:]...)
	out = append(out, flags)
	var ctr [4]byte
	binary.BigEndian.PutUint32(ctr[:], counter)
	out = append(out, ctr[:]...)
	if att != nil {
		if len(att.AAGUID) != 16 {
			return nil, errors.New("fake: AAGUID must be 16 bytes")
		}
		out = append(out, att.AAGUID...)
		var credLen [2]byte
		binary.BigEndian.PutUint16(credLen[:], uint16(len(att.CredentialID)))
		out = append(out, credLen[:]...)
		out = append(out, att.CredentialID...)
		out = append(out, att.CredentialPublicKey...)
	}
	return out, nil
}

// buildCOSEKey produces the COSE_Key map for an ES256 public key.
// The keys are IANA COSE integers; values come from the ECDSA point.
func buildCOSEKey(pub ecdsa.PublicKey) map[int64]any {
	// P-256 curve coordinates are exactly 32 bytes each; pad with zeros
	// on the left if the big.Int's Bytes() dropped leading zeros.
	x := padTo(pub.X.Bytes(), 32)
	y := padTo(pub.Y.Bytes(), 32)
	return map[int64]any{
		1:  int64(2),  // kty: EC2
		3:  int64(-7), // alg: ES256
		-1: int64(1),  // crv: P-256
		-2: x,
		-3: y,
	}
}

// padTo left-pads b with zeros so the result is exactly n bytes long.
// big.Int.Bytes() drops leading zeros, which would produce a malformed
// coordinate in the COSE_Key map.
func padTo(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}

// extractChallenge pulls the challenge string out of the JSON the RP
// returned from BeginRegistration / BeginLogin. The shape is:
//
//	{"publicKey": {"challenge": "<b64url>"}}
//
// The challenge is returned as-is (the b64url string) because that is
// exactly what CollectedClientData.Challenge carries.
func extractChallenge(rpJSON []byte, _ string) (string, error) {
	var outer struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(rpJSON, &outer); err != nil {
		return "", fmt.Errorf("fake: parse rp response: %w", err)
	}
	if outer.PublicKey.Challenge == "" {
		return "", errors.New("fake: rp response missing challenge")
	}
	return outer.PublicKey.Challenge, nil
}

// Compile-time sanity: ensure *big.Int is imported even when the file is
// refactored (the sign path uses big.Int indirectly via ECDSA).
var _ = big.NewInt
