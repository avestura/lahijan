// Package powerdns_test: records_test.go covers RRset CRUD + the reverse-zone
// helpers against the fake PowerDNS server.
package powerdns_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedZone creates a zone named "<t.Name()>-<suffix>.example.com." and
// returns the canonical id the driver expects on subsequent calls. Using
// the test name keeps parallel tests from colliding on a global namespace.
func seedZone(t *testing.T, p *powerdns.Provider, ctx context.Context, slug string) string {
	t.Helper()
	name := slug + ".example.com."
	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        name,
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)
	return z.ID
}

func TestRRset_ReplaceAndSearch(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-crud")

	require.NoError(t, p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID: zoneID,
		Name:   "www.rrset-crud.example.com.",
		Type:   powerdns.TypeA,
		TTL:    300,
		Records: []powerdns.Record{
			{Content: "192.0.2.1"},
			{Content: "192.0.2.2"},
		},
	}))

	rrsets, err := p.SearchRRsets(ctx, zoneID, "www.rrset-crud.example.com.", powerdns.TypeA)
	require.NoError(t, err)
	require.Len(t, rrsets, 1)
	assert.Equal(t, "www.rrset-crud.example.com.", rrsets[0].Name)
	assert.Equal(t, "A", rrsets[0].Type)
	assert.Equal(t, 300, rrsets[0].TTL)
	assert.Len(t, rrsets[0].Records, 2)
}

func TestRRset_Replace_ReplacesInPlace(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-replace")
	name := "www.rrset-replace.example.com."

	require.NoError(t, p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID:  zoneID,
		Name:    name,
		Type:    powerdns.TypeA,
		TTL:     300,
		Records: []powerdns.Record{{Content: "192.0.2.1"}},
	}))
	// Replace with a new IP list — the old record must be gone.
	require.NoError(t, p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID:  zoneID,
		Name:    name,
		Type:    powerdns.TypeA,
		TTL:     300,
		Records: []powerdns.Record{{Content: "192.0.2.5"}},
	}))

	rrsets, err := p.SearchRRsets(ctx, zoneID, name, powerdns.TypeA)
	require.NoError(t, err)
	require.Len(t, rrsets, 1)
	require.Len(t, rrsets[0].Records, 1)
	assert.Equal(t, "192.0.2.5", rrsets[0].Records[0].Content)
}

func TestRRset_Delete(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-del")
	name := "delete-me.rrset-del.example.com."

	require.NoError(t, p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID: zoneID, Name: name, Type: powerdns.TypeA, TTL: 60,
		Records: []powerdns.Record{{Content: "192.0.2.10"}},
	}))
	require.NoError(t, p.DeleteRRset(ctx, powerdns.DeleteRRsetParams{
		ZoneID: zoneID, Name: name, Type: powerdns.TypeA,
	}))

	rrsets, err := p.SearchRRsets(ctx, zoneID, name, powerdns.TypeA)
	require.NoError(t, err)
	assert.Empty(t, rrsets, "deleted RRset must not appear in search")
}

func TestRRset_Validation_UnsupportedType(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-type")
	err := p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID:  zoneID,
		Name:    "x.rrset-type.example.com.",
		Type:    powerdns.RecordType("NAPTR"),
		TTL:     60,
		Records: []powerdns.Record{{Content: "junk"}},
	})
	require.Error(t, err, "unsupported record types must be rejected client-side")
}

func TestRRset_Validation_NameNotInZone(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-zone")
	err := p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID:  zoneID,
		Name:    "www.different-zone.example.com.",
		Type:    powerdns.TypeA,
		TTL:     60,
		Records: []powerdns.Record{{Content: "192.0.2.1"}},
	})
	require.Error(t, err, "name outside the zone must be rejected client-side")
}

func TestRRset_AllSupportedTypes(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	zoneID := seedZone(t, p, ctx, "rrset-types")
	cases := []struct {
		rtype   powerdns.RecordType
		name    string
		content string
	}{
		{powerdns.TypeA, "a.rrset-types.example.com.", "192.0.2.1"},
		{powerdns.TypeAAAA, "aaaa.rrset-types.example.com.", "2001:db8::1"},
		{powerdns.TypeCAA, "caa.rrset-types.example.com.", "0 issue \"letsencrypt.org\""},
		{powerdns.TypeCNAME, "cname.rrset-types.example.com.", "target.example.com."},
		{powerdns.TypeMX, "mx.rrset-types.example.com.", "10 mail.example.com."},
		{powerdns.TypeNS, "ns.rrset-types.example.com.", "ns1.example.com."},
		{powerdns.TypeSRV, "srv.rrset-types.example.com.", "10 5 5060 sip.example.com."},
		{powerdns.TypeTLSA, "tlsa.rrset-types.example.com.", "3 1 1 abcdef"},
		{powerdns.TypeTXT, "txt.rrset-types.example.com.", "\"v=spf1 -all\""},
	}
	for _, tc := range cases {
		err := p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
			ZoneID: zoneID, Name: tc.name, Type: tc.rtype, TTL: 300,
			Records: []powerdns.Record{{Content: tc.content}},
		})
		require.NoError(t, err, "type %s content %q", tc.rtype, tc.content)
	}
}

func TestReverseZone_IPv4(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cidr string
		want string
	}{
		{"192.0.2.0/24", "2.0.192.in-addr.arpa."},
		{"10.0.0.0/8", "10.in-addr.arpa."},
		{"172.16.0.0/12", "172.in-addr.arpa."},
		{"203.0.113.32/27", "113.0.203.in-addr.arpa."},
	}
	for _, tc := range cases {
		got, err := powerdns.ReverseZoneName(tc.cidr)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "cidr %s", tc.cidr)
	}
}

func TestReverseZone_IPv6(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cidr string
		want string
	}{
		{"2001:db8::/32", "8.b.d.0.1.0.0.2.ip6.arpa."},
		{"2001:db8::/48", "0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa."},
	}
	for _, tc := range cases {
		got, err := powerdns.ReverseZoneName(tc.cidr)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "cidr %s", tc.cidr)
	}
}

func TestReverseZone_InvalidCIDR(t *testing.T) {
	t.Parallel()
	_, err := powerdns.ReverseZoneName("not-a-cidr")
	require.Error(t, err)
}

func TestPTRName_IPv4(t *testing.T) {
	t.Parallel()
	got, err := powerdns.PTRName("192.0.2.5")
	require.NoError(t, err)
	assert.Equal(t, "5.2.0.192.in-addr.arpa.", got)
}

func TestPTRName_IPv6(t *testing.T) {
	t.Parallel()
	got, err := powerdns.PTRName("2001:db8::1")
	require.NoError(t, err)
	assert.Equal(t,
		"1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa.",
		got)
}

func TestPTRName_Invalid(t *testing.T) {
	t.Parallel()
	_, err := powerdns.PTRName("not-an-ip")
	require.Error(t, err, "invalid IP must be rejected")
	assert.True(t, errors.Is(err, powerdns.ErrOperationFailed) || err != nil,
		"should error")
}
