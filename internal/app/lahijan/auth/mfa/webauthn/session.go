// Package webauthn: session.go serialises the WebAuthn SessionData the
// RP returns from Begin* into a stable JSON []byte for storage, and
// deserialises it back for the matching Finish* call.
//
// The MFA service stores the marshalled bytes on the mfa_pending_sessions
// row (or a dedicated ceremony-session row) keyed by the user + a fresh
// opaque challenge token. The browser hands the challenge token back at
// /finish; the service looks up the row, unmarshals the session, and
// hands it to FinishRegistration / FinishLogin.
package webauthn

import (
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/webauthn"
)

// SessionData is the opaque, JSON-serialisable ceremony session the RP
// mints at Begin* and consumes at Finish*. The package exposes it as a
// []byte wrapper so callers do not need to import the upstream library.
type SessionData struct {
	// raw is the JSON-encoded form of *webauthn.SessionData. Callers
	// persist it as-is and hand it back unchanged.
	raw []byte
}

// MarshalSession serialises the upstream SessionData pointer into the
// package's opaque SessionData wrapper. Caller-stable across processes.
func MarshalSession(s *webauthn.SessionData) (SessionData, error) {
	if s == nil {
		return SessionData{}, fmt.Errorf("webauthn: marshal nil session")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return SessionData{}, fmt.Errorf("webauthn: marshal session: %w", err)
	}
	return SessionData{raw: raw}, nil
}

// UnmarshalSession deserialises the package's SessionData wrapper back
// into the upstream SessionData pointer the RP's Finish* methods expect.
func UnmarshalSession(s SessionData) (*webauthn.SessionData, error) {
	if len(s.raw) == 0 {
		return nil, fmt.Errorf("webauthn: unmarshal empty session")
	}
	var out webauthn.SessionData
	if err := json.Unmarshal(s.raw, &out); err != nil {
		return nil, fmt.Errorf("webauthn: unmarshal session: %w", err)
	}
	return &out, nil
}

// Raw returns the underlying JSON bytes. Used by the MFA service to
// persist the session: it stores Raw() in a TEXT/bytea column and rebuilds
// the SessionData via UnmarshalSession at Finish* time.
func (s SessionData) Raw() []byte { return s.raw }

// String is a debug helper; never logs the contents (the session carries
// the challenge, which is sensitive).
func (s SessionData) String() string {
	if len(s.raw) == 0 {
		return "SessionData(empty)"
	}
	return "SessionData(<redacted>)"
}

// SessionFromBytes builds a SessionData wrapper from raw bytes the caller
// persisted. The bytes must come from a previous MarshalSession / Raw()
// call; otherwise UnmarshalSession will reject them.
func SessionFromBytes(b []byte) SessionData { return SessionData{raw: b} }
