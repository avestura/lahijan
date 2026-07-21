// Package registrar: json.go is a tiny indirection over encoding/json so
// the service.go file does not need to import the package directly (the
// import block is already large).
package registrar

import "encoding/json"

// jsonEncode is the alias the service file uses for json.Marshal.
func jsonEncode(v any) ([]byte, error) {
	return json.Marshal(v)
}
