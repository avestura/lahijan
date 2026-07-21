// billing_payments_test_helpers_test.go contains shared helpers used by
// the WS-27 service-level tests. Kept separate so the main test file
// stays focused on test cases.
package billing_test

import "net/http"

// httpClientForTest returns a fresh *http.Client for the test stripe
// provider. Default timeout is generous; tests that need a tighter
// cap can override.
func httpClientForTest() *http.Client {
	return &http.Client{}
}
