// Package program: agent_meter.go adapts billing.Service to the agent.Meter
// seam so admin-provided agent turns are metered (WS-31c): every turn records
// a usage_event (resource_type "agent_token") and debits the ledger via the
// canonical PostCharge path, and the spend-cap pre-check reads the cached
// balance. BYOK turns never reach this adapter.
package program

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/agent"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// agentMeter wires billing.Service into the agent.Service as a Meter. It is
// safe for concurrent use (it wraps stateless service / repo calls).
type agentMeter struct {
	billing   *billing.Service
	usage     *database.BillingUsageRepository
	ratePer1k int64 // ledger cents per 1000 model tokens (conf)
}

// newAgentMeter builds the adapter. ratePer1k is the conf-driven placeholder
// price; a value <= 0 disables charging (turns are still metered as events).
func newAgentMeter(
	billingSvc *billing.Service,
	usage *database.BillingUsageRepository,
	ratePer1k int64,
) *agentMeter {
	return &agentMeter{billing: billingSvc, usage: usage, ratePer1k: ratePer1k}
}

// BalanceCents reports the user's cached balance for the spend-cap pre-check.
func (m *agentMeter) BalanceCents(ctx context.Context, userID uuid.UUID) (int64, error) {
	bal, err := m.billing.GetBalance(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("agent.meter.balance: %w", err)
	}
	return bal.BalanceCents, nil
}

// ChargeAgentTokens records a usage_event and debits the ledger for one
// admin-provided turn. The reference doubles as the idempotency key on both
// rows so a retried/duplicated turn cannot double-count.
func (m *agentMeter) ChargeAgentTokens(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	u agent.TurnUsage,
	reference string,
) error {
	if u.TotalTokens == 0 {
		return nil // nothing to charge; provider reported no usage.
	}
	now := time.Now().UTC()
	// usage_events (raw metering stream; WS-17 reconciles into pricing).
	if _, err := m.usage.Create(ctx, database.CreateUsageEventParams{
		UserID:         userID,
		ResourceType:   "agent_token",
		Qty:            int64(u.TotalTokens),
		Unit:           "tokens",
		StartedAt:      now,
		EndedAt:        now,
		IdempotencyKey: &reference,
	}); err != nil {
		return fmt.Errorf("agent.meter.usage_event: %w", err)
	}
	if m.ratePer1k <= 0 {
		return nil // metering only; no charge configured.
	}
	cents := m.ratePer1k * int64(u.TotalTokens) / 1000
	if cents < 1 {
		cents = 1 // minimum charge so a fractional-token turn still debits.
	}
	if _, err := m.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
		UserID:         userID,
		AmountCents:    cents,
		Currency:       "USD",
		Reference:      reference,
		IdempotencyKey: &reference,
		Metadata: map[string]any{
			"source":            "agent",
			"prompt_tokens":     u.PromptTokens,
			"completion_tokens": u.CompletionTokens,
			"total_tokens":      u.TotalTokens,
		},
	}); err != nil {
		return fmt.Errorf("agent.meter.charge: %w", err)
	}
	return nil
}
