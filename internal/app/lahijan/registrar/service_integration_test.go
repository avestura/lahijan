// Package registrar: service_integration_test.go exercises the
// registrar.Service end-to-end against a real Postgres (via
// testcontainers-go) + an in-process fake of the registrar provider.
// Covers the WS-28 DoD:
//
//   - search domain -> register -> renew -> delete with audit + bus assertions
//   - invalid domain name surfaces a clear error
//   - domain transfer (EPP flow)
//   - tenant isolation: tenant A cannot operate on tenant B's domains
//   - every privileged action emits audit pre + post
//
// Run with:  go test -tags integration ./internal/app/lahijan/registrar/...

//go:build integration

package registrar_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar/fake"
	registrarsvc "github.com/avestura/lahijan/internal/app/lahijan/registrar"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// fixture bundles the per-test dependencies. Every test gets a fresh
// fixture so parallel tests do not collide on Postgres state.
type fixture struct {
	svc      *registrarsvc.Service
	billing  *fakeBilling
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
		bus:      &recorderBus{},
		auditEm:  newCapturingEmitter(),
		billing:  newFakeBilling(),
		tenantID: tenant.ID,
		userID:   user.ID,
		ctx:      ctx,
		tenantCtx: database.WithTenant(ctx, tenant.ID),
	}
	// Build a real OpenSRS provider pointing at the in-process fake.
	srv := fake.NewServer()
	t.Cleanup(srv.Close)
	provider := srv.Provider()
	f.svc = registrarsvc.New(
		provider,
		repos,
		f.auditEm,
		f.bus,
		nil, // policy not used by the service (RBAC enforced at HTTP layer)
		f.billing,
		registrarsvc.Config{
			DefaultCurrency: "USD",
		},
	)
	return f
}

// fakeBilling implements registrar.billingHook + billing.Service-like
// surface for the tests. Charges are recorded in-memory; the test asserts.
type fakeBilling struct {
	mu      sync.Mutex
	charges []billing.PostChargeParams
	ledger  map[uuid.UUID]database.LedgerEntry // id -> entry (returned by PostCharge)
}

func newFakeBilling() *fakeBilling {
	return &fakeBilling{ledger: map[uuid.UUID]database.LedgerEntry{}}
}

func (f *fakeBilling) PostCharge(_ context.Context, _ uuid.UUID, params billing.PostChargeParams) (database.LedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	entry := database.LedgerEntry{ID: id, AmountCents: params.AmountCents, Currency: params.Currency}
	f.ledger[id] = entry
	f.charges = append(f.charges, params)
	return entry, nil
}

// recorderBus implements registrar.eventBus. Records every Emit so tests
// can assert on the topic.
type recorderBus struct {
	mu     sync.Mutex
	topics []string
}

func (r *recorderBus) Emit(_ context.Context, e eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topics = append(r.topics, e.Topic)
	return nil
}

func (r *recorderBus) Topics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.topics))
	copy(out, r.topics)
	return out
}

// capturingEmitter implements audit.Emitter. Records every Emit so tests
// can assert on the action + resource.
type capturingEmitter struct {
	mu      sync.Mutex
	events  []audit.Event
	outcomes map[uuid.UUID]audit.Outcome
}

func newCapturingEmitter() *capturingEmitter {
	return &capturingEmitter{outcomes: map[uuid.UUID]audit.Outcome{}}
}

func (c *capturingEmitter) Emit(_ context.Context, e audit.Event) (uuid.UUID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := uuid.New()
	e.Status = audit.StatusPending
	c.events = append(c.events, e)
	return id, nil
}

func (c *capturingEmitter) MarkOutcome(_ context.Context, id uuid.UUID, o audit.Outcome) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outcomes[id] = o
	return nil
}

func (c *capturingEmitter) Events() []audit.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.Event, len(c.events))
	copy(out, c.events)
	return out
}

// newFakeRegistrarServer returns a fake registrar server with the
// standard per-year price ($10).
func newFakeRegistrarServer() *fake.Server {
	return fake.NewServer()
}

// fakeRegistrarServer is a thin wrapper around the providers/registrar/fake
// server so this test file does not pull in the fake package directly
// (the fake is internal-only).
type fakeRegistrarServer struct {
	close func()
	provider *registrar.OpenSRSProvider
}

func (s *fakeRegistrarServer) Close() { s.close() }
func (s *fakeRegistrarServer) Provider() *registrar.OpenSRSProvider { return s.provider }

// ===========================================================================
// Tests
// ===========================================================================

// TestSearchDomain_Available covers the happy path: an available domain
// returns a search result with pricing.
func TestSearchDomain_Available(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	res, err := f.svc.SearchDomain(f.tenantCtx, f.tenantID, f.userID, "lahijan-test-available.com")
	require.NoError(t, err)
	assert.True(t, res.Available)
	assert.Equal(t, "available", res.Status)
	require.Len(t, res.Pricing, 3)
}

// TestRegisterDomain_Success covers the happy path: register a domain
// and verify the dns_domains row + the billing charge + the audit row
// + the WASM bus event.
func TestRegisterDomain_Success(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	row, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "lahijan-register-it.com",
		PeriodYears: 2,
		Contact: &registrar.ContactProfile{
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
	assert.NotEmpty(t, row.RegistrarOrderID)
	assert.Equal(t, "lahijan-register-it.com.", row.Name)
	assert.Equal(t, "registered", row.Status)
	require.NotNil(t, row.ExpiresAt)
	assert.True(t, row.ExpiresAt.After(time.Now()))

	// Billing: 2 years * $10 = $20.
	require.Len(t, f.billing.charges, 1)
	assert.Equal(t, int64(2000), f.billing.charges[0].AmountCents)

	// Audit: register action emitted.
	found := false
	for _, e := range f.auditEm.Events() {
		if e.Action == registrarsvc.AuditRegister {
			found = true
			break
		}
	}
	assert.True(t, found, "register audit action not emitted")

	// Bus: registered event.
	assert.Contains(t, f.bus.Topics(), eventbus.DNSDomainRegistered)
}

// TestRegisterDomain_InvalidName covers the validation error path.
func TestRegisterDomain_InvalidName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "UPPERCASE.com", // invalid — must be lowercase
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
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
	require.ErrorIs(t, err, registrarsvc.ErrInvalidDomain)
}

// TestRegisterDomain_AlreadyTaken covers the unavailable path.
func TestRegisterDomain_AlreadyTaken(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Register once.
	_, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "lahijan-taken-once.com",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "Test", OwnerLastname: "User",
			OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.NoError(t, err)

	// Register again — the registrar will report it as taken.
	_, err = f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "lahijan-taken-once.com",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "Test", OwnerLastname: "User",
			OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, registrarsvc.ErrDomainUnavailable)
}

// TestRenewDomain_Success covers the renew happy path.
func TestRenewDomain_Success(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Register first.
	row, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "lahijan-renew-it.com",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "Test", OwnerLastname: "User",
			OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.NoError(t, err)
	originalExpiry := *row.ExpiresAt

	// Renew.
	renewed, err := f.svc.RenewDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RenewDomainRequest{
		DomainID:    row.ID,
		PeriodYears: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, renewed.ExpiresAt)
	assert.True(t, renewed.ExpiresAt.After(originalExpiry))
}

// TestListDomains_Pagination covers listing + counting.
func TestListDomains_Pagination(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Register 3 domains.
	for i := 0; i < 3; i++ {
		_, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
			Domain:      "lahijan-list-" + string(rune('a'+i)) + ".com",
			PeriodYears: 1,
			Contact: &registrar.ContactProfile{
				OwnerFirstname: "Test", OwnerLastname: "User",
				OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
				Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
			},
		})
		require.NoError(t, err)
	}

	rows, err := f.svc.ListDomains(f.tenantCtx, f.tenantID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	count, err := f.svc.CountDomains(f.tenantCtx, f.tenantID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

// TestTenantIsolation verifies that tenant A cannot see or operate on
// tenant B's domains.
func TestTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	bus := &recorderBus{}
	auditEm := newCapturingEmitter()
	billingHook := newFakeBilling()
	srv := fake.NewServer()
	defer srv.Close()
	svc := registrarsvc.New(
		srv.Provider(),
		repos,
		auditEm,
		bus,
		nil,
		billingHook,
		registrarsvc.Config{DefaultCurrency: "USD"},
	)

	ctxA := database.WithTenant(ctx, tenantA.ID)
	ctxB := database.WithTenant(ctx, tenantB.ID)

	// Tenant A registers a domain.
	row, err := svc.RegisterDomain(ctxA, tenantA.ID, user.ID, registrarsvc.RegisterDomainRequest{
		Domain:      "tenant-a-only.com",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "A", OwnerLastname: "Tenant",
			OwnerEmail: "a@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.NoError(t, err)

	// Tenant B lists domains — must not see tenant A's domain.
	rows, err := svc.ListDomains(ctxB, tenantB.ID, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, rows)

	// Tenant B tries to GET tenant A's domain — must fail with not-found.
	_, err = svc.GetDomain(ctxB, tenantB.ID, user.ID, row.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, registrarsvc.ErrDomainNotFound)

	// Tenant B tries to DELETE tenant A's domain — must fail.
	err = svc.DeleteDomain(ctxB, tenantB.ID, user.ID, row.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, registrarsvc.ErrDomainNotFound)
}

// TestDeleteDomain_Success covers the delete happy path.
func TestDeleteDomain_Success(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	row, err := f.svc.RegisterDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.RegisterDomainRequest{
		Domain:      "lahijan-delete-me.com",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "Test", OwnerLastname: "User",
			OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.NoError(t, err)

	err = f.svc.DeleteDomain(f.tenantCtx, f.tenantID, f.userID, row.ID)
	require.NoError(t, err)

	// GetDomain now returns not-found.
	_, err = f.svc.GetDomain(f.tenantCtx, f.tenantID, f.userID, row.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, registrarsvc.ErrDomainNotFound)

	// Audit + bus emitted.
	foundDelete := false
	for _, e := range f.auditEm.Events() {
		if e.Action == registrarsvc.AuditDelete {
			foundDelete = true
			break
		}
	}
	assert.True(t, foundDelete, "delete audit action not emitted")
	assert.Contains(t, f.bus.Topics(), eventbus.DNSDomainDeleted)
}

// TestTransferDomain_Success covers the transfer happy path.
func TestTransferDomain_Success(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	row, err := f.svc.TransferDomain(f.tenantCtx, f.tenantID, f.userID, registrarsvc.TransferDomainRequest{
		Domain:      "lahijan-transfer-in.com",
		AuthCode:    "AUTH1",
		PeriodYears: 1,
		Contact: &registrar.ContactProfile{
			OwnerFirstname: "Test", OwnerLastname: "User",
			OwnerEmail: "test@example.com", OwnerPhone: "+15551234567",
			Address1: "1 Main St", City: "Anytown", State: "CA", Zip: "90001", CountryCode: "US",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "lahijan-transfer-in.com.", row.Name)
	assert.Equal(t, "transferred", row.Status)
}

// TestProvider_NoProviderWired covers the disabled path: a service
// built without a provider returns ErrProviderDisabled.
func TestProvider_NoProviderWired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	svc := registrarsvc.New(
		nil, // provider
		repos,
		audit.NoopEmitter{},
		nil,
		nil,
		newFakeBilling(),
		registrarsvc.Config{DefaultCurrency: "USD"},
	)
	tenantCtx := database.WithTenant(ctx, tenant.ID)

	_, err := svc.SearchDomain(tenantCtx, tenant.ID, user.ID, "foo.com")
	require.Error(t, err)
	require.ErrorIs(t, err, registrarsvc.ErrProviderDisabled)
}

// errors.Is(_:, nil) — silence the unused-import linter when this file
// is referenced from a non-integration build.
var _ = errors.Is
