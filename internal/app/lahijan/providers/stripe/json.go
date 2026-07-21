// Package stripe: json.go is a tiny encoding/json shim that lets the
// rest of the package avoid importing encoding/json everywhere (the
// client + the webhook verifier are the only files that need it; the
// area files use json via the do() decoder).
package stripe

import "encoding/json"

// jsonUnmarshalBytes wraps json.Unmarshal so the package has a single
// JSON-decode seam. Tests that want to inject a decoder swap this out
// via package-internal state.
func jsonUnmarshalBytes(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
