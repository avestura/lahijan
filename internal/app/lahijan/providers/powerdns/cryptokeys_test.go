// Package powerdns_test: cryptokeys_test.go covers DNSSEC key CRUD + the
// enable/disable convenience helpers against the fake PowerDNS server.
package powerdns_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCryptoKey_EnableDisable(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "dnssec.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	// Freshly-created zone must not have DNSSEC.
	on, err := p.IsDNSSECEnabled(ctx, z.ID)
	require.NoError(t, err)
	assert.False(t, on, "fresh zone must have DNSSEC disabled")

	// Enable DNSSEC; the helper creates one combined signing key.
	created, err := p.EnableDNSSEC(ctx, z.ID)
	require.NoError(t, err)
	assert.True(t, created.Active, "EnableDNSSEC must create an active key")
	assert.True(t, created.Published, "EnableDNSSEC must publish the DNSKEY")

	// IsDNSSECEnabled now reports true.
	on, err = p.IsDNSSECEnabled(ctx, z.ID)
	require.NoError(t, err)
	assert.True(t, on)

	// DisableDNSSEC removes every key.
	require.NoError(t, p.DisableDNSSEC(ctx, z.ID))
	on, err = p.IsDNSSECEnabled(ctx, z.ID)
	require.NoError(t, err)
	assert.False(t, on, "DisableDNSSEC must clear every active key")
}

func TestCryptoKey_Enable_IsIdempotent(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "idem.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	first, err := p.EnableDNSSEC(ctx, z.ID)
	require.NoError(t, err)
	// Calling EnableDNSSEC a second time must not create a second key —
	// it should return the existing active key.
	second, err := p.EnableDNSSEC(ctx, z.ID)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "EnableDNSSEC must be idempotent")

	keys, err := p.ListCryptoKeys(ctx, z.ID)
	require.NoError(t, err)
	assert.Len(t, keys, 1, "idempotent EnableDNSSEC must not duplicate keys")
}

func TestCryptoKey_ManualCreateAndToggle(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "manual.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	key, err := p.CreateCryptoKey(ctx, powerdns.CryptoKeyParams{
		ZoneID:    z.ID,
		KeyType:   "ksk",
		Algorithm: "ecdsaP256SHA256",
		Bits:      256,
		Active:    true,
	})
	require.NoError(t, err)
	assert.True(t, key.Active)

	// Deactivate via ToggleCryptoKey.
	require.NoError(t, p.ToggleCryptoKey(ctx, z.ID, key.ID, false))

	got, err := p.GetCryptoKey(ctx, z.ID, key.ID, false)
	require.NoError(t, err)
	assert.False(t, got.Active, "ToggleCryptoKey(false) must deactivate")
}

func TestCryptoKey_Delete(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "del-key.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	key, err := p.CreateCryptoKey(ctx, powerdns.CryptoKeyParams{
		ZoneID:  z.ID,
		KeyType: "zsk",
		Active:  true,
	})
	require.NoError(t, err)

	require.NoError(t, p.DeleteCryptoKey(ctx, z.ID, key.ID))
	keys, err := p.ListCryptoKeys(ctx, z.ID)
	require.NoError(t, err)
	assert.Empty(t, keys, "deleted key must not appear in the list")
}
