// Package billing: receipts_test.go validates the hand-rolled PDF
// generator without standing up a database. The byte-level shape +
// the structural invariants (%PDF- header, xref table, trailer) are
// checked so a regression in the writer fails loudly.
package billing

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReceiptPDF_ProducesValidPDF(t *testing.T) {
	t.Parallel()
	pdf := buildReceiptPDF(receiptPDFParams{
		TenantID:     uuid.New(),
		UserID:       uuid.New(),
		PeriodStart:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:    time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC),
		TotalCents:   1234,
		BalanceCents: 500,
		Currency:     "USD",
		GeneratedAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})

	// PDF 1.4 header.
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF-1.4")),
		"PDF must start with the %PDF-1.4 header, got: %q", pdf[:min(10, len(pdf))])

	// xref + trailer + EOF present (basic structural invariants).
	str := string(pdf)
	assert.Contains(t, str, "xref")
	assert.Contains(t, str, "trailer")
	assert.Contains(t, str, "/Root 1 0 R")
	assert.Contains(t, str, "startxref")
	require.True(t, bytes.HasSuffix(pdf, []byte("%%EOF\n")),
		"PDF must end with %%EOF, got tail: %q", pdf[max(0, len(pdf)-10):])

	// Content invariants: the period dates + total appear in the body.
	assert.Contains(t, str, "Billing Receipt")
	assert.Contains(t, str, "Total charged:")
	assert.Contains(t, str, "Balance at end:")
	assert.Contains(t, str, "2026-07-01")

	// Sanity: a receipt is small (< 4 KiB). If we ever cross this
	// threshold, the in-row BYTEA storage decision should be
	// revisited.
	assert.Less(t, len(pdf), 4096, "receipt PDF should be small")
}

func TestBuildReceiptPDF_NegativeBalanceRendersAbs(t *testing.T) {
	t.Parallel()
	pdf := buildReceiptPDF(receiptPDFParams{
		TenantID:     uuid.New(),
		UserID:       uuid.New(),
		PeriodStart:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:    time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC),
		TotalCents:   999,
		BalanceCents: -250,
		Currency:     "USD",
		GeneratedAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	// The receipt shows abs values so the cents digits render the
	// same regardless of sign; the test asserts no double-escape
	// artefacts appear (a regression in escapePDFText would produce
	// "--" or similar).
	assert.NotContains(t, string(pdf), "--")
}

func TestEscapePDFText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"a(b", "a\\(b"},
		{"a)b", "a\\)b"},
		{"a\\b", "a\\\\b"},
		{"a\nb", "a\\nb"},
		{"a\rb", "a\\rb"},
		// Non-Latin characters are replaced with '?' (WinAnsi
		// Helvetica limitation).
		{"fa-test", "fa-test"},
		{"héllo", "h?llo"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, escapePDFText(tc.in))
		})
	}
}

func TestValidateAmount(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateAmount(1))
	assert.NoError(t, validateAmount(100))
	assert.ErrorIs(t, validateAmount(0), ErrInvalidAmount)
	assert.ErrorIs(t, validateAmount(-1), ErrInvalidAmount)
}

func TestValidateResource(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateResource("compute.cpu", "core-hours"))
	assert.ErrorIs(t, validateResource("", "x"), ErrInvalidResource)
	assert.ErrorIs(t, validateResource("x", ""), ErrInvalidResource)
}

func TestValidateCurrency(t *testing.T) {
	t.Parallel()
	assert.NoError(t, validateCurrency("USD"))
	assert.NoError(t, validateCurrency("usd"))
	assert.ErrorIs(t, validateCurrency("US"), ErrInvalidCurrency)
	assert.ErrorIs(t, validateCurrency("USDD"), ErrInvalidCurrency)
	assert.ErrorIs(t, validateCurrency("U1D"), ErrInvalidCurrency)
}

// absCents helper test — keep separate from validateAmount so the
// intent (handling negative balances in the PDF) is documented.
func TestAbsCents(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(0), absCents(0))
	assert.Equal(t, int64(42), absCents(42))
	assert.Equal(t, int64(42), absCents(-42))
}
