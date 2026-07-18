// Package seaweedfs: json.go is a tiny stable-name wrapper around
// encoding/json so call sites in iam.go + quotas.go can reference the
// marshal/unmarshal helpers by a single, greppable name. The wrapper
// exists only to make future observability hooks (e.g. tracing the
// marshal of a request body) a single-line change.
package seaweedfs

import "encoding/json"

// jsonMarshalImpl is encoding/json.Marshal.
func jsonMarshalImpl(v any) ([]byte, error) {
	return json.Marshal(v)
}

// jsonUnmarshalImpl is encoding/json.Unmarshal.
func jsonUnmarshalImpl(body []byte, v any) error {
	return json.Unmarshal(body, v)
}
