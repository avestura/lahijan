// Package compute: snapshots_test.go covers the pure-Go paths in
// snapshots.go (cadence parser, FormatCadenceHuman) + the
// WS-25-specific error sentinels. The DB-backed snapshot lifecycle is
// exercised by service_integration_test.go (build tag: integration).
package compute

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCadence_Hourly(t *testing.T) {
	t.Parallel()
	d, err := ParseCadence("PT1H")
	require.NoError(t, err)
	assert.Equal(t, time.Hour, d)
}

func TestParseCadence_Daily(t *testing.T) {
	t.Parallel()
	d, err := ParseCadence("P1D")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, d)
}

func TestParseCadence_Weekly(t *testing.T) {
	t.Parallel()
	d, err := ParseCadence("P1W")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, d)
}

func TestParseCadence_Combined(t *testing.T) {
	t.Parallel()
	d, err := ParseCadence("P1DT2H")
	require.NoError(t, err)
	assert.Equal(t, 26*time.Hour, d)
}

func TestParseCadence_Minutes(t *testing.T) {
	t.Parallel()
	d, err := ParseCadence("PT30M")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, d)
}

func TestParseCadence_RejectsBadShapes(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "garbage", "P", "P0D", "PT0H", "P1Y", "P1M"} {
		_, err := ParseCadence(in)
		require.ErrorIs(t, err, ErrInvalidCadence, "input %q must be rejected", in)
	}
}

func TestParseCadence_RejectsZeroDuration(t *testing.T) {
	t.Parallel()
	// P0D parses but yields zero duration; reject so a misconfigured policy
	// does not loop the worker hot.
	_, err := ParseCadence("P0D")
	require.ErrorIs(t, err, ErrInvalidCadence)
}

func TestFormatCadenceHuman_Passthrough(t *testing.T) {
	t.Parallel()
	// FormatCadenceHuman returns the raw string for known shapes; we do
	// not localise here.
	assert.Equal(t, "PT1H", FormatCadenceHuman("PT1H"))
	assert.Equal(t, "garbage", FormatCadenceHuman("garbage"))
}

func TestErrSentinels_DistinctValues(t *testing.T) {
	t.Parallel()
	// The HTTP error mapper switches on errors.Is; the sentinels MUST be
	// distinct or a 404 might mask a 409. This test guards against a
	// refactor that aliases two of them by mistake.
	all := []error{
		ErrSnapshotNotFound, ErrSnapshotNameTaken,
		ErrBackupTargetNotFound, ErrBackupTargetNameTaken, ErrBackupNotFound,
		ErrSnapshotPolicyNotFound, ErrSnapshotPolicyNameTaken,
		ErrInvalidCadence, ErrCryptoRequired, ErrUnknownBackupTargetKind,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			assert.NotSame(t, a, b, "sentinels %d + %d must not alias", i, j)
		}
	}
}
