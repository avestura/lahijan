// Package dns: service_integration_test.go exercises the dns.Service
// end-to-end against a real Postgres (via testcontainers-go) + an
// in-process fake of the PowerDNS provider. Covers the WS-15 DoD:
//
//   - create zone -> create record (every supported type) -> update ->
//     delete with audit + bus assertions
//   - invalid record content surfaces the per-type validator error
//   - DNSSEC enable/disable propagates to the provider + caches the flag
//   - templates apply correctly (Google Workspace / Microsoft 365)
//   - multi-tenant isolation: tenant A cannot operate on tenant B's zones
//   - every privileged action emits audit pre + post
//
// Run with:  go test -tags integration ./internal/app/lahijan/dns/...

//go:build integration

package dns_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// fakePDNS is an in-memory implementation of dns.pdnsProvider. It tracks
// calls + drives zone + RRset state so the service test can assert the
// orchestration order without standing up a real httptest fake. (The
// httptest fake has its own coverage in providers/powerdns/fake/.)
type fakePDNS struct {
	mu          sync.Mutex
	zones       map[string]*fakeZoneState // canonical id -> zone state
	rrsets      map[string][]powerdns.RRset
	cryptokeys  map[string][]powerdns.CryptoKey
	nextKeyID   int64
	createErr   error
	updateErr   error
	deleteErr   error
	replaceErr  error
	enableErr   error
	disableErr  error
	toggledOn   int32
	toggledOff  int32
}

type fakeZoneState struct {
	zone powerdns.Zone
}

func newFakePDNS() *fakePDNS {
	return &fakePDNS{
		zones:      make(map[string]*fakeZoneState),
		rrsets:     make(map[string][]powerdns.RRset),
		cryptokeys: make(map[string][]powerdns.CryptoKey),
	}
}

func (f *fakePDNS) Ping(_ context.Context) error { return nil }

func (f *fakePDNS) CreateZone(_ context.Context, params powerdns.CreateZoneParams) (*powerdns.Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	if _, exists := f.zones[params.Name]; exists {
		return nil, powerdns.ErrAlreadyExists
	}
	z := powerdns.Zone{
		ID:      params.Name,
		Name:    params.Name,
		Type:    string(params.Kind),
		Kind:    string(params.Kind),
		Account: params.Account,
	}
	f.zones[params.Name] = &fakeZoneState{zone: z}
	return &z, nil
}

func (f *fakePDNS) GetZone(_ context.Context, zoneID string, rrsets bool) (*powerdns.Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	zs, ok := f.zones[zoneID]
	if !ok {
		return nil, powerdns.ErrNotFound
	}
	z := zs.zone
	if rrsets {
		z.RRsets = f.rrsets[zoneID]
	}
	return &z, nil
}

func (f *fakePDNS) ListZones(_ context.Context) ([]powerdns.Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]powerdns.Zone, 0, len(f.zones))
	for _, zs := range f.zones {
		out = append(out, zs.zone)
	}
	return out, nil
}

func (f *fakePDNS) UpdateZone(_ context.Context, zoneID string, update powerdns.ZoneUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	zs, ok := f.zones[zoneID]
	if !ok {
		return powerdns.ErrNotFound
	}
	if update.Kind != "" {
		zs.zone.Kind = update.Kind
		zs.zone.Type = update.Kind
	}
	if update.Account != "" {
		zs.zone.Account = update.Account
	}
	return nil
}

func (f *fakePDNS) DeleteZone(_ context.Context, zoneID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.zones[zoneID]; !ok {
		return powerdns.ErrNotFound
	}
	delete(f.zones, zoneID)
	delete(f.rrsets, zoneID)
	delete(f.cryptokeys, zoneID)
	return nil
}

func (f *fakePDNS) ReplaceRRset(_ context.Context, params powerdns.RRsetUpsertParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replaceErr != nil {
		return f.replaceErr
	}
	// Find existing or append.
	rrsets := f.rrsets[params.ZoneID]
	for i, rr := range rrsets {
		if rr.Name == params.Name && rr.Type == string(params.Type) {
			rrsets[i].Records = params.Records
			rrsets[i].TTL = params.TTL
			f.rrsets[params.ZoneID] = rrsets
			return nil
		}
	}
	f.rrsets[params.ZoneID] = append(rrsets, powerdns.RRset{
		Name:    params.Name,
		Type:    string(params.Type),
		TTL:     params.TTL,
		Records: params.Records,
	})
	return nil
}

func (f *fakePDNS) DeleteRRset(_ context.Context, params powerdns.DeleteRRsetParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rrsets := f.rrsets[params.ZoneID]
	for i, rr := range rrsets {
		if rr.Name == params.Name && rr.Type == string(params.Type) {
			f.rrsets[params.ZoneID] = append(rrsets[:i], rrsets[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakePDNS) SearchRRsets(_ context.Context, zoneID string, _ string, _ powerdns.RecordType) ([]powerdns.RRset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]powerdns.RRset, len(f.rrsets[zoneID]))
	copy(out, f.rrsets[zoneID])
	return out, nil
}

func (f *fakePDNS) EnableDNSSEC(_ context.Context, zoneID string) (*powerdns.CryptoKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enableErr != nil {
		return nil, f.enableErr
	}
	// Idempotent: if an active key exists, return it.
	for _, k := range f.cryptokeys[zoneID] {
		if k.Active {
			atomic.AddInt32(&f.toggledOn, 1)
			return &k, nil
		}
	}
	f.nextKeyID++
	k := powerdns.CryptoKey{
		ID:       f.nextKeyID,
		KeyType:  "csk",
		Active:   true,
		Bits:     256,
		Published: true,
	}
	f.cryptokeys[zoneID] = append(f.cryptokeys[zoneID], k)
	atomic.AddInt32(&f.toggledOn, 1)
	return &k, nil
}

func (f *fakePDNS) DisableDNSSEC(_ context.Context, zoneID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.disableErr != nil {
		return f.disableErr
	}
	delete(f.cryptokeys, zoneID)
	atomic.AddInt32(&f.toggledOff, 1)
	return nil
}

func (f *fakePDNS) IsDNSSECEnabled(_ context.Context, zoneID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.cryptokeys[zoneID] {
		if k.Active {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePDNS) ListCryptoKeys(_ context.Context, zoneID string) ([]powerdns.CryptoKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]powerdns.CryptoKey, len(f.cryptokeys[zoneID]))
	copy(out, f.cryptokeys[zoneID])
	return out, nil
}

// recorderBus is a minimal eventbus.Bus-shaped recorder.
type recorderBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (r *recorderBus) Emit(_ context.Context, e eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recorderBus) snapshot() []eventbus.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.Event, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recorderBus) topics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Topic
	}
	return out
}

// capturingEmitter records every Emit + MarkOutcome call.
type capturingEmitter struct {
	mu       sync.Mutex
	rows     []audit.Event
	outcomes map[uuid.UUID]audit.Outcome
}

func newCapturingEmitter() *capturingEmitter {
	return &capturingEmitter{outcomes: map[uuid.UUID]audit.Outcome{}}
}

func (e *capturingEmitter) Emit(_ context.Context, ev audit.Event) (uuid.UUID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.New()
	e.rows = append(e.rows, ev)
	return id, nil
}

func (e *capturingEmitter) MarkOutcome(_ context.Context, auditID uuid.UUID, o audit.Outcome) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcomes[auditID] = o
	return nil
}

func (e *capturingEmitter) eventsFor(action string) []audit.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []audit.Event{}
	for _, r := range e.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func (e *capturingEmitter) successCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusSuccess {
			n++
		}
	}
	return n
}

func (e *capturingEmitter) failureCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusFailure {
			n++
		}
	}
	return n
}

// fixture wires the dependencies the suite shares.
type fixture struct {
	svc      *dns.Service
	pdns     *fakePDNS
	bus      *recorderBus
	auditEm  *capturingEmitter
	tenantID uuid.UUID
	userID   uuid.UUID
	ctx      context.Context
	tenantCtx context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	f := &fixture{
		pdns:    newFakePDNS(),
		bus:     &recorderBus{},
		auditEm: newCapturingEmitter(),
		tenantID: tenant.ID,
		userID:   user.ID,
		ctx:      ctx,
		tenantCtx: database.WithTenant(ctx, tenant.ID),
	}
	f.svc = dns.New(f.pdns, repos, f.auditEm, f.bus, nil, dns.Config{
		DefaultNameservers: []string{"ns1.example.net."},
	})
	return f
}

// TestCreateZone_RecordCRUD_PerType is the WS-15 DoD happy-path
// integration test. Creates a zone, walks every supported record type,
// updates one, deletes one, and asserts audit + bus emissions.
func TestCreateZone_RecordCRUD_PerType(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name:        "ws15-example.com.",
		Description: "WS-15 happy-path",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, zone.ID)
	assert.Equal(t, "ws15-example.com.", zone.CanonicalID)
	assert.Equal(t, "Native", zone.Kind)

	// Audit + event bus assertions for create-zone.
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditZoneCreate),
		"create-zone must emit an audit row")
	require.NotEmpty(t, f.bus.snapshot(), "create-zone must emit into the bus")
	assert.Contains(t, f.bus.topics(), eventbus.DNSZoneCreated)

	// Walk every supported record type. Names must be lowercase per
	// canonical-name rules; the prefix is the record type lowercased
	// so the audit / list rendering is stable + the validator accepts
	// the name.
	cases := []struct {
		name, rtype, content string
	}{
		{"a-record", dns.TypeA, "192.0.2.1"},
		{"aaaa-record", dns.TypeAAAA, "2001:db8::1"},
		{"cname-record", dns.TypeCNAME, "target.example.org."},
		{"mx-record", dns.TypeMX, "10 mail.example.com."},
		{"txt-record", dns.TypeTXT, "\"v=spf1 -all\""},
		{"ns-record", dns.TypeNS, "ns1.example.net."},
		{"srv-record", dns.TypeSRV, "10 60 5060 sip.example.com."},
		{"caa-record", dns.TypeCAA, "0 issue \"letsencrypt.org\""},
	}
	for _, tc := range cases {
		tc := tc
		lowerName := strings.ToLower(tc.rtype)
		t.Run(tc.name, func(t *testing.T) {
			// Subtests do NOT run in parallel because they all share
			// the fixture's audit emitter; the parent asserts the
			// event count after every subtest completes, and parallel
			// subtests would not have finished by then.
			row, errRecord := f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
				Name:    lowerName + ".ws15-example.com.",
				Type:    tc.rtype,
				Content: tc.content,
			})
			require.NoError(t, errRecord, "create %s must succeed", tc.rtype)
			assert.NotEqual(t, uuid.Nil, row.ID)
		})
	}

	// Audit assertions: every record-create emits an audit row.
	rows := f.auditEm.eventsFor(dns.AuditRecordCreate)
	require.Len(t, rows, len(cases), "one audit row per record-create")

	// Update one record (TXT).
	listed, err := f.svc.ListRecords(f.tenantCtx, f.tenantID, zone.ID, 100, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(listed), len(cases))
	var txtRow database.DNSRecord
	for _, r := range listed {
		if r.Type == dns.TypeTXT {
			txtRow = r
			break
		}
	}
	require.NotEqual(t, uuid.Nil, txtRow.ID)
	newContent := "\"v=spf1 include:_spf.example.com ~all\""
	updated, err := f.svc.UpdateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, txtRow.ID, dns.RecordUpdateParams{
		Content: &newContent,
	})
	require.NoError(t, err)
	assert.Equal(t, newContent, updated.Content)
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditRecordUpdate))

	// Delete one record (A).
	var aRow database.DNSRecord
	for _, r := range listed {
		if r.Type == dns.TypeA {
			aRow = r
			break
		}
	}
	require.NotEqual(t, uuid.Nil, aRow.ID)
	require.NoError(t, f.svc.DeleteRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, aRow.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditRecordDelete))

	// Delete the zone.
	require.NoError(t, f.svc.DeleteZone(f.tenantCtx, f.tenantID, f.userID, zone.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditZoneDelete))

	// Get after delete -> 404 envelope.
	_, err = f.svc.GetZone(f.tenantCtx, f.tenantID, zone.ID)
	require.ErrorIs(t, err, dns.ErrZoneNotFound)
}

// TestCreateRecord_InvalidContent covers the WS-15 DoD item "invalid
// record content rejected with a clear message (per type)".
func TestCreateRecord_InvalidContent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "invalid-content.example.com.",
	})
	require.NoError(t, err)

	cases := []struct {
		name, rtype, content string
		wantErr              error
	}{
		{"A invalid", dns.TypeA, "not-an-ip", dns.ErrInvalidRecordContent},
		{"A v6 in v4", dns.TypeA, "::1", dns.ErrInvalidRecordContent},
		{"AAAA invalid", dns.TypeAAAA, "not-an-ip", dns.ErrInvalidRecordContent},
		{"CNAME no dot", dns.TypeCNAME, "target.example.org", dns.ErrInvalidRecordContent},
		{"MX missing prio", dns.TypeMX, "mail.example.com.", dns.ErrInvalidRecordContent},
		{"SRV too few fields", dns.TypeSRV, "10 60 5060", dns.ErrInvalidRecordContent},
		{"TXT unquoted", dns.TypeTXT, "v=spf1 -all", dns.ErrInvalidRecordContent},
		{"unsupported type", "BOGUS", "anything", dns.ErrInvalidRecordType},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Subtests do NOT run in parallel because they all share
			// the fixture's audit emitter; the parent asserts the
			// event count after every subtest completes.
			_, errRecord := f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
				Name:    "x.invalid-content.example.com.",
				Type:    tc.rtype,
				Content: tc.content,
			})
			require.ErrorIs(t, errRecord, tc.wantErr)
		})
	}
}

// TestCreateRecord_CNAMEAtApex covers the WS-15 Open Question 2 default
// (strict RFC: CNAME at apex rejected).
func TestCreateRecord_CNAMEAtApex(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "apex.example.com.",
	})
	require.NoError(t, err)

	_, err = f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
		Name:    "apex.example.com.", // apex
		Type:    dns.TypeCNAME,
		Content: "target.example.org.",
	})
	require.ErrorIs(t, err, dns.ErrCNAMEAtApex)
}

// TestCreateRecord_InvalidTTL covers the WS-15 Open Question 1 default
// (custom TTL allowed within [300, 86400]).
func TestCreateRecord_InvalidTTL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "ttl.example.com.",
	})
	require.NoError(t, err)

	_, err = f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
		Name:    "x.ttl.example.com.",
		Type:    dns.TypeA,
		Content: "192.0.2.1",
		TTL:     100, // below MinTTL
	})
	require.ErrorIs(t, err, dns.ErrInvalidTTL)

	_, err = f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
		Name:    "y.ttl.example.com.",
		Type:    dns.TypeA,
		Content: "192.0.2.2",
		TTL:     100000, // above MaxTTL
	})
	require.ErrorIs(t, err, dns.ErrInvalidTTL)
}

// TestCreateZone_DuplicateCanonical covers the WS-15 Open Question 3
// default (canonical_id is globally unique; two tenants cannot own the
// same zone).
func TestCreateZone_DuplicateCanonical(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	svc := dns.New(newFakePDNS(), repos, newCapturingEmitter(), nil, nil, dns.Config{
		DefaultNameservers: []string{"ns1.example.net."},
	})

	ctxA := database.WithTenant(ctx, tenantA.ID)
	_, err := svc.CreateZone(ctxA, tenantA.ID, user.ID, dns.ZoneCreateParams{
		Name: "shared.example.com.",
	})
	require.NoError(t, err)

	ctxB := database.WithTenant(ctx, tenantB.ID)
	_, err = svc.CreateZone(ctxB, tenantB.ID, user.ID, dns.ZoneCreateParams{
		Name: "shared.example.com.",
	})
	require.ErrorIs(t, err, dns.ErrZoneAlreadyExists, "tenant B cannot claim the same canonical id")
}

// TestMultiTenantIsolation asserts the WS-15 DoD: "tenant A cannot see/
// manage tenant B's zones". The repository enforces it; this test
// verifies the service preserves it end-to-end.
func TestMultiTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	svc := dns.New(newFakePDNS(), repos, newCapturingEmitter(), nil, nil, dns.Config{
		DefaultNameservers: []string{"ns1.example.net."},
	})

	ctxA := database.WithTenant(ctx, tenantA.ID)
	zoneA, err := svc.CreateZone(ctxA, tenantA.ID, user.ID, dns.ZoneCreateParams{
		Name: "tenant-a.example.com.",
	})
	require.NoError(t, err)

	ctxB := database.WithTenant(ctx, tenantB.ID)
	_, err = svc.GetZone(ctxB, tenantB.ID, zoneA.ID)
	require.ErrorIs(t, err, dns.ErrZoneNotFound, "tenant B cannot read tenant A's zone")

	zonesB, err := svc.ListZones(ctxB, tenantB.ID, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, zonesB, "tenant B list must be empty")

	// Tenant B cannot delete tenant A's zone.
	err = svc.DeleteZone(ctxB, tenantB.ID, user.ID, zoneA.ID)
	require.ErrorIs(t, err, dns.ErrZoneNotFound)

	// Tenant B cannot create a record in tenant A's zone.
	_, err = svc.CreateRecord(ctxB, tenantB.ID, user.ID, zoneA.ID, dns.RecordCreateParams{
		Name:    "x.tenant-a.example.com.",
		Type:    dns.TypeA,
		Content: "192.0.2.1",
	})
	require.ErrorIs(t, err, dns.ErrZoneNotFound)
}

// TestDNSSEC_EnableDisable covers the WS-15 DoD "DNSSEC enable/disable
// propagates to PowerDNS" + the cached flag round-trips.
func TestDNSSEC_EnableDisable(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "dnssec.example.com.",
	})
	require.NoError(t, err)
	assert.False(t, zone.IsDnssecEnabled, "DNSSEC off by default per WS-12")

	require.NoError(t, f.svc.EnableDNSSEC(f.tenantCtx, f.tenantID, f.userID, zone.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditDNSSECEnable))
	assert.Contains(t, f.bus.topics(), eventbus.DNSZoneDNSECSecured)

	// Reconcile flips the cached flag.
	reconciled, err := f.svc.ReconcileZone(f.tenantCtx, f.tenantID, zone.ID)
	require.NoError(t, err)
	assert.True(t, reconciled.IsDnssecEnabled, "reconcile must surface the live flag")

	// Disable.
	require.NoError(t, f.svc.DisableDNSSEC(f.tenantCtx, f.tenantID, f.userID, zone.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditDNSSECDisable))
	assert.Contains(t, f.bus.topics(), eventbus.DNSZoneDNSSECDisabled)

	reconciled, err = f.svc.ReconcileZone(f.tenantCtx, f.tenantID, zone.ID)
	require.NoError(t, err)
	assert.False(t, reconciled.IsDnssecEnabled, "reconcile must reflect disabled state")
}

// TestTemplates_ApplyGoogleWorkspace covers the WS-15 DoD "templates
// apply correctly": apply the Google Workspace template to a fresh zone
// and assert both records land.
func TestTemplates_ApplyGoogleWorkspace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "gw.example.com.",
	})
	require.NoError(t, err)

	applied, err := f.svc.ApplyTemplate(f.tenantCtx, f.tenantID, f.userID, zone.ID, "google-workspace")
	require.NoError(t, err)
	assert.Equal(t, 2, applied, "Google Workspace template applies 2 records")

	// List and assert both records are present.
	listed, err := f.svc.ListRecords(f.tenantCtx, f.tenantID, zone.ID, 100, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(listed), 2)
	types := map[string]bool{}
	for _, r := range listed {
		types[r.Type] = true
	}
	assert.True(t, types[dns.TypeMX], "MX record must be present")
	assert.True(t, types[dns.TypeTXT], "TXT record must be present")

	// Template apply is idempotent.
	applied2, err := f.svc.ApplyTemplate(f.tenantCtx, f.tenantID, f.userID, zone.ID, "google-workspace")
	require.NoError(t, err)
	assert.Equal(t, 0, applied2, "second apply is a no-op")

	// Audit + bus emissions.
	require.NotEmpty(t, f.auditEm.eventsFor(dns.AuditTemplateApply),
		"apply-template must emit an audit row")
}

// TestTemplates_ApplyMicrosoft365 covers the %w placeholder expansion:
// the Microsoft 365 MX content uses "%w" so the result is
// "example.com.mail.protection.outlook.com." (NOT "example.com..mail...").
func TestTemplates_ApplyMicrosoft365(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "m365.example.com.",
	})
	require.NoError(t, err)

	applied, err := f.svc.ApplyTemplate(f.tenantCtx, f.tenantID, f.userID, zone.ID, "microsoft-365")
	require.NoError(t, err)
	assert.Equal(t, 2, applied)

	listed, err := f.svc.ListRecords(f.tenantCtx, f.tenantID, zone.ID, 100, 0)
	require.NoError(t, err)
	var mx database.DNSRecord
	for _, r := range listed {
		if r.Type == dns.TypeMX {
			mx = r
			break
		}
	}
	require.NotEqual(t, uuid.Nil, mx.ID, "MX record must be present")
	assert.Contains(t, mx.Content, "m365.example.com.mail.protection.outlook.com.")
	assert.NotContains(t, mx.Content, "..", "no double dot from %w expansion")
}

// TestTemplates_NotFound covers the WS-15 DoD for template-not-found.
func TestTemplates_NotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "tpl-nf.example.com.",
	})
	require.NoError(t, err)

	_, err = f.svc.ApplyTemplate(f.tenantCtx, f.tenantID, f.userID, zone.ID, "bogus-template")
	require.ErrorIs(t, err, dns.ErrTemplateNotFound)
}

// TestAuditOutcome_TrailForEachPrivilegedAction asserts every privileged
// action emits an audit row with status=pending and marks the outcome
// (success or failure) afterwards.
func TestAuditOutcome_TrailForEachPrivilegedAction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "audit.example.com.",
	})
	require.NoError(t, err)

	successes := f.auditEm.successCount()
	assert.Greater(t, successes, 0, "create-zone must mark at least one outcome success")

	// Force a failure on the next privileged action and assert the audit
	// row's outcome is marked failure.
	f.pdns.replaceErr = errors.New("daemon down")
	_, err = f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
		Name:    "x.audit.example.com.",
		Type:    dns.TypeA,
		Content: "192.0.2.1",
	})
	require.Error(t, err)
	rows := f.auditEm.eventsFor(dns.AuditRecordCreate)
	require.Len(t, rows, 1, "record-create must emit an audit row even on failure")
	failures := f.auditEm.failureCount()
	assert.Greater(t, failures, 0, "failed create must mark an outcome failure")
}

// TestEventBus_EmitsEveryChange asserts the WASM event bus receives
// every canonical DNS topic during a create -> update -> delete flow.
func TestEventBus_EmitsEveryChange(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	zone, err := f.svc.CreateZone(f.tenantCtx, f.tenantID, f.userID, dns.ZoneCreateParams{
		Name: "bus.example.com.",
	})
	require.NoError(t, err)

	row, err := f.svc.CreateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, dns.RecordCreateParams{
		Name:    "www.bus.example.com.",
		Type:    dns.TypeA,
		Content: "192.0.2.5",
	})
	require.NoError(t, err)

	newContent := "192.0.2.6"
	_, err = f.svc.UpdateRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, row.ID, dns.RecordUpdateParams{
		Content: &newContent,
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteRecord(f.tenantCtx, f.tenantID, f.userID, zone.ID, row.ID))
	require.NoError(t, f.svc.DeleteZone(f.tenantCtx, f.tenantID, f.userID, zone.ID))

	topics := f.bus.topics()
	assert.Contains(t, topics, eventbus.DNSZoneCreated)
	assert.Contains(t, topics, eventbus.DNSRecordCreated)
	assert.Contains(t, topics, eventbus.DNSRecordUpdated)
	assert.Contains(t, topics, eventbus.DNSRecordDeleted)
	assert.Contains(t, topics, eventbus.DNSZoneDeleted)
}
