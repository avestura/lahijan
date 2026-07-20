// Package stripe: form_helpers.go is a tiny fmt-backed shim used by
// customers.go + payment_methods.go so the package does not pull
// encoding/json into the area files. Kept separate so the dependency
// surface stays obvious.
package stripe

import "fmt"

// fmtSPrint wraps fmt.Sprint so we don't import fmt at the top of
// every area file. Used by toString() for the catch-all metadata
// value formatting.
func fmtSPrint(v any) string { return fmt.Sprint(v) }
