// codes_test.go covers the status-code constants + the Explain label
// helper. Pure-Go unit tests; no DB / wazero.

package hostfuncs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExplain_KnownCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code int32
		want string
	}{
		{StatusSuccess, "success"},
		{StatusGenericFailure, "generic_failure"},
		{StatusDenied, "denied"},
		{StatusUnavailable, "unavailable"},
		{StatusInvalidMemory, "invalid_memory"},
		{StatusInvalidArgument, "invalid_argument"},
		{StatusNotFound, "not_found"},
		{StatusBufferTooSmall, "buffer_too_small"},
		{StatusUpstreamError, "upstream_error"},
		{42, "ok_with_length"}, // positive code = success-with-length
		{-99, "unknown"},       // unknown negative
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Explain(tc.code))
		})
	}
}

func TestStatusCodes_AreStable(t *testing.T) {
	t.Parallel()
	// These constants form the plugin-facing ABI; the values must NOT
	// change across versions (ADR-0024). Pin them so a refactor does
	// not silently break plugins compiled against an older SDK.
	assert.Equal(t, int32(0), StatusSuccess)
	assert.Equal(t, int32(-1), StatusGenericFailure)
	assert.Equal(t, int32(-2), StatusDenied)
	assert.Equal(t, int32(-3), StatusUnavailable)
	assert.Equal(t, int32(-4), StatusInvalidMemory)
	assert.Equal(t, int32(-5), StatusInvalidArgument)
	assert.Equal(t, int32(-6), StatusNotFound)
	assert.Equal(t, int32(-7), StatusBufferTooSmall)
	assert.Equal(t, int32(-8), StatusUpstreamError)
}
