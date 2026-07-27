// Package agent: metering_test.go locks down the WS-31c metering gate + the
// spend-cap pre-check without spinning up the billing service (a fake Meter
// stands in).
package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// fakeMeter records ChargeAgentTokens calls and returns a fixed balance.
type fakeMeter struct {
	balance    int64
	balanceErr error
	charges    []chargeCall
	chargeErr  error
}

type chargeCall struct {
	tenantID, userID uuid.UUID
	usage            TurnUsage
	reference        string
}

func (m *fakeMeter) BalanceCents(_ context.Context, _ uuid.UUID) (int64, error) {
	return m.balance, m.balanceErr
}

func (m *fakeMeter) ChargeAgentTokens(_ context.Context, tenantID, userID uuid.UUID, u TurnUsage, ref string) error {
	m.charges = append(m.charges, chargeCall{tenantID: tenantID, userID: userID, usage: u, reference: ref})
	return m.chargeErr
}

func newSvcWithMeter(m Meter) *Service {
	return &Service{
		config: Config{Enabled: true},
		meter:  m,
	}
}

// TestMeterTurn_AdminCharges confirms an admin-provided turn with reported
// usage drives exactly one charge carrying the token totals.
func TestMeterTurn_AdminCharges(t *testing.T) {
	t.Parallel()
	m := &fakeMeter{}
	s := newSvcWithMeter(m)

	ctx := database.WithTenant(context.Background(), uuid.New())
	uid := uuid.New()
	s.meterTurn(ctx, uid, uuid.New(), uuid.New(),
		ResolvedProvider{AdminProvided: true}, &TurnUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})

	if len(m.charges) != 1 {
		t.Fatalf("want 1 charge, got %d", len(m.charges))
	}
	if m.charges[0].usage.TotalTokens != 150 {
		t.Fatalf("usage not forwarded: %+v", m.charges[0].usage)
	}
	if m.charges[0].userID != uid {
		t.Fatalf("user id not forwarded")
	}
	if m.charges[0].reference == "" {
		t.Fatalf("reference (idempotency key) must be set")
	}
}

// TestMeterTurn_SkipsBYOKAndMissingUsage confirms BYOK turns, nil usage, and
// a missing meter never charge.
func TestMeterTurn_SkipsBYOKAndMissingUsage(t *testing.T) {
	t.Parallel()
	m := &fakeMeter{}
	s := newSvcWithMeter(m)
	ctx := database.WithTenant(context.Background(), uuid.New())
	uid := uuid.New()
	u := &TurnUsage{TotalTokens: 10}

	cases := []struct {
		name     string
		provider ResolvedProvider
		usage    *TurnUsage
		meter    Meter
	}{
		{"byok", ResolvedProvider{AdminProvided: false}, u, m},
		{"no_usage", ResolvedProvider{AdminProvided: true}, nil, m},
		{"no_meter", ResolvedProvider{AdminProvided: true}, u, nil},
	}
	for _, c := range cases {
		s.meter = c.meter
		s.meterTurn(ctx, uid, uuid.New(), uuid.New(), c.provider, c.usage)
		if len(m.charges) != 0 {
			t.Fatalf("%s: expected no charge, got %d", c.name, len(m.charges))
		}
	}
}

// TestEnforceSpendCap covers the four-way decision: admin vs BYOK crossed
// with funded vs empty balance.
func TestEnforceSpendCap(t *testing.T) {
	t.Parallel()
	ctx := database.WithTenant(context.Background(), uuid.New())
	uid := uuid.New()
	capped := Policy{SpendCapCredits: 1000}

	cases := []struct {
		name     string
		provider ResolvedProvider
		balance  int64
		wantErr  error
	}{
		{"byok_funded", ResolvedProvider{AdminProvided: false}, 500, nil},
		{"byok_empty", ResolvedProvider{AdminProvided: false}, 0, nil},
		{"admin_funded", ResolvedProvider{AdminProvided: true}, 500, nil},
		{"admin_empty", ResolvedProvider{AdminProvided: true}, 0, ErrSpendCap},
		{"admin_negative", ResolvedProvider{AdminProvided: true}, -50, ErrSpendCap},
	}
	for _, c := range cases {
		m := &fakeMeter{balance: c.balance}
		s := newSvcWithMeter(m)
		err := s.enforceSpendCap(ctx, uid, c.provider, capped)
		if c.wantErr == nil && err != nil {
			t.Fatalf("%s: want nil, got %v", c.name, err)
		}
		if c.wantErr != nil && !errors.Is(err, c.wantErr) {
			t.Fatalf("%s: want %v, got %v", c.name, c.wantErr, err)
		}
	}
}

// TestEnforceSpendCap_NoCapAllowsZeroBalance confirms a zero/absent cap does
// not reject an admin turn even when the balance is zero (uncapped tenants).
func TestEnforceSpendCap_NoCapAllowsZeroBalance(t *testing.T) {
	t.Parallel()
	ctx := database.WithTenant(context.Background(), uuid.New())
	m := &fakeMeter{balance: 0}
	s := newSvcWithMeter(m)
	if err := s.enforceSpendCap(ctx, uuid.New(), ResolvedProvider{AdminProvided: true}, Policy{}); err != nil {
		t.Fatalf("uncapped admin turn must not be rejected: %v", err)
	}
}
