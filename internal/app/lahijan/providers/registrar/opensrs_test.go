// Package registrar: opensrs_test.go exercises the OpenSRSProvider
// against the in-process fake server. Every Provider method is covered
// for the success + error path.
package registrar_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar/fake"
)

// TestProvider_Noop asserts the Noop provider returns ErrDisabled on
// every method. This is the bootstrap fallback when no registrar is
// configured.
func TestProvider_Noop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := registrar.NoopProvider{}

	require.ErrorIs(t, p.Ping(ctx), registrar.ErrDisabled)
	_, err := p.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
	_, err = p.RegisterDomain(ctx, registrar.RegisterDomainRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
	_, err = p.RenewDomain(ctx, registrar.RenewDomainRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
	_, err = p.TransferDomain(ctx, registrar.TransferDomainRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
	_, err = p.GetDomain(ctx, registrar.GetDomainRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
	_, err = p.SetDSRecords(ctx, registrar.SetDSRecordsRequest{Domain: "example.com"})
	require.ErrorIs(t, err, registrar.ErrDisabled)
}

// TestProvider_Name verifies the OpenSRS provider reports "opensrs".
func TestProvider_Name(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()
	assert.Equal(t, "opensrs", p.Name())
}

// TestProvider_Capabilities verifies the OpenSRS capability set.
func TestProvider_Capabilities(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()
	caps := p.Capabilities()
	assert.Equal(t, "opensrs", caps.ProviderName)
	assert.True(t, caps.SupportsDNSSEC)
	assert.True(t, caps.SupportsTransfers)
	assert.True(t, caps.SupportsWHOISPrivacy)
	assert.True(t, caps.SupportsAutoRenew)
}

// TestProvider_Ping covers the ping path.
func TestProvider_Ping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()
	require.NoError(t, p.Ping(ctx))
}

// TestProvider_CheckDomain_Available covers the available-search path.
func TestProvider_CheckDomain_Available(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()

	resp, err := p.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: "lahijan-test-available.com"})
	require.NoError(t, err)
	assert.True(t, resp.Available)
	assert.Equal(t, registrar.StatusAvailable, resp.Status)
	require.Len(t, resp.Pricing, 3)
	assert.Equal(t, int32(1), resp.Pricing[0].PeriodYears)
	assert.Equal(t, int64(1000), resp.Pricing[0].PriceCents)
	assert.Equal(t, "USD", resp.Pricing[0].Currency)
}

// TestProvider_CheckDomain_Taken covers the taken-search path.
func TestProvider_CheckDomain_Taken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	expires := time.Now().UTC().AddDate(1, 0, 0)
	srv.Seed(registrar.GetDomainResponse{
		Domain:    "lahijan-test-taken.com",
		OrderID:   "PRE-EXISTING-1",
		Status:    "registered",
		ExpiresAt: &expires,
	})
	p := srv.Provider()

	resp, err := p.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: "lahijan-test-taken.com"})
	require.NoError(t, err)
	assert.False(t, resp.Available)
	assert.Equal(t, registrar.StatusUnavailable, resp.Status)
}

// TestProvider_Register_Success covers the register-then-get path.
func TestProvider_Register_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()

	resp, err := p.RegisterDomain(ctx, registrar.RegisterDomainRequest{
		Domain:      "lahijan-register-success.com",
		PeriodYears: 2,
		Contact: registrar.ContactProfile{
			OwnerFirstname: "Test",
			OwnerLastname:  "User",
			OwnerEmail:     "test@example.com",
			OwnerPhone:     "+15551234567",
			Address1:       "1 Main St",
			City:           "Anytown",
			State:          "CA",
			Zip:            "90001",
			CountryCode:    "US",
		},
		AutoRenew:    true,
		WHOISPrivacy: true,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.OrderID)
	assert.Equal(t, "lahijan-register-success.com", resp.Domain)
	assert.Equal(t, registrar.StatusRegistered, resp.Status)
	require.NotNil(t, resp.ExpiresAt)
	assert.Equal(t, int64(2000), resp.PriceCents) // 2 years * 1000
	assert.Equal(t, "USD", resp.Currency)

	// GetDomain returns the same state.
	got, err := p.GetDomain(ctx, registrar.GetDomainRequest{Domain: "lahijan-register-success.com"})
	require.NoError(t, err)
	assert.Equal(t, resp.OrderID, got.OrderID)
	assert.True(t, got.AutoRenew)
	assert.True(t, got.PrivacyOn)
}

// TestProvider_Register_AlreadyExists covers the conflict path.
func TestProvider_Register_AlreadyExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	expires := time.Now().UTC().AddDate(1, 0, 0)
	srv.Seed(registrar.GetDomainResponse{
		Domain:    "lahijan-register-duplicate.com",
		OrderID:   "PRE-EXISTING-2",
		Status:    "registered",
		ExpiresAt: &expires,
	})
	p := srv.Provider()

	_, err := p.RegisterDomain(ctx, registrar.RegisterDomainRequest{
		Domain:      "lahijan-register-duplicate.com",
		PeriodYears: 1,
		Contact: registrar.ContactProfile{
			OwnerFirstname: "Test",
			OwnerLastname:  "User",
			OwnerEmail:     "test@example.com",
			OwnerPhone:     "+15551234567",
			Address1:       "1 Main St",
			City:           "Anytown",
			State:          "CA",
			Zip:            "90001",
			CountryCode:    "US",
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, registrar.ErrAlreadyExists)
}

// TestProvider_Renew_Success covers the renew-extends-expiry path.
func TestProvider_Renew_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	// Seed a registered domain with order id + expiry.
	expires := time.Now().UTC().AddDate(0, 6, 0) // 6 months left
	srv.Seed(registrar.GetDomainResponse{
		Domain:    "lahijan-renew-success.com",
		OrderID:   "PRE-EXISTING-3",
		Status:    "registered",
		ExpiresAt: &expires,
	})
	p := srv.Provider()

	resp, err := p.RenewDomain(ctx, registrar.RenewDomainRequest{
		Domain:      "lahijan-renew-success.com",
		OrderID:     "PRE-EXISTING-3",
		PeriodYears: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, "lahijan-renew-success.com", resp.Domain)
	assert.Equal(t, registrar.StatusRegistered, resp.Status)
	require.NotNil(t, resp.ExpiresAt)
	assert.True(t, resp.ExpiresAt.After(expires))
}

// TestProvider_Transfer_Success covers the transfer path.
func TestProvider_Transfer_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	p := srv.Provider()

	resp, err := p.TransferDomain(ctx, registrar.TransferDomainRequest{
		Domain:      "lahijan-transfer-success.com",
		AuthCode:    "AUTH1",
		PeriodYears: 1,
		Contact: registrar.ContactProfile{
			OwnerFirstname: "Test",
			OwnerLastname:  "User",
			OwnerEmail:     "test@example.com",
			OwnerPhone:     "+15551234567",
			Address1:       "1 Main St",
			City:           "Anytown",
			State:          "CA",
			Zip:            "90001",
			CountryCode:    "US",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, registrar.StatusTransferred, resp.Status)
}

// TestProvider_SetDSRecords_Success covers the DS-record publish path.
func TestProvider_SetDSRecords_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	expires := time.Now().UTC().AddDate(1, 0, 0)
	srv.Seed(registrar.GetDomainResponse{
		Domain:    "lahijan-ds-success.com",
		OrderID:   "PRE-EXISTING-4",
		Status:    "registered",
		ExpiresAt: &expires,
	})
	p := srv.Provider()

	resp, err := p.SetDSRecords(ctx, registrar.SetDSRecordsRequest{
		Domain: "lahijan-ds-success.com",
		Records: []registrar.DSRecord{
			{KeyTag: 12345, Algorithm: 13, DigestType: 2, Digest: "abc01237890abcdef"},
		},
	})
	require.NoError(t, err)
	assert.True(t, resp.Applied)
	assert.EqualValues(t, 1, resp.RecordCount)
}

// TestProvider_NewOpenSRS_MissingFields asserts the constructor
// rejects every missing required field.
func TestProvider_NewOpenSRS_MissingFields(t *testing.T) {
	t.Parallel()
	_, err := registrar.NewOpenSRSProvider(registrar.OpenSRSConfig{})
	require.Error(t, err)

	_, err = registrar.NewOpenSRSProvider(registrar.OpenSRSConfig{
		HTTPClient: nil,
		BaseURL:    "http://example.com",
		APIKey:     "k",
		Username:   "u",
	})
	require.Error(t, err)
}

// TestProvider_CheckDomain_FailInjected covers the FailOn seam.
func TestProvider_CheckDomain_FailInjected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := fake.NewServer()
	defer srv.Close()
	srv.FailOn = map[string]error{
		"POST domains/lookup": errors.New("injected"),
	}
	p := srv.Provider()

	_, err := p.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: "fail.com"})
	require.Error(t, err)
}
