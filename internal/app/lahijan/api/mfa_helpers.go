// Package api: mfa_helpers.go provides the small encoding helpers used by
// mfa_handlers.go to round-trip the WebAuthn ceremony session between the
// api layer and the browser. Kept in a separate file so the handlers stay
// readable.
package api

import "encoding/base64"

// webauthnB64 base64-encodes a byte slice for HTTP transport. Used to
// ship the ceremony session between /begin and /finish as a JSON string.
func webauthnB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// webauthnB64Decode is the inverse of webauthnB64.
func webauthnB64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
