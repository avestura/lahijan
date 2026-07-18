// Package database: plugin_event_subscriptions_repo.go wraps the
// sqlc-generated plugin_event_subscriptions queries (WS-10b). The event bus
// consults this table at emit time to find the match list for a topic.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// PluginEventSubscriptionsRepository is the persistence boundary for the
// plugin_event_subscriptions table.
type PluginEventSubscriptionsRepository struct {
	q *gen.Queries
}

// NewPluginEventSubscriptionsRepository wraps the given sqlc queries.
func NewPluginEventSubscriptionsRepository(q *gen.Queries) *PluginEventSubscriptionsRepository {
	return &PluginEventSubscriptionsRepository{q: q}
}

// CreateSubscriptionParams carries the user-controlled fields of a
// subscription row.
type CreateSubscriptionParams struct {
	TenantID     *uuid.UUID
	PluginID     uuid.UUID
	TopicPattern string
	Handler      string
}

// Subscribe idempotently adds a subscription. Returns the row, or a zero
// gen.PluginEventSubscription with no error when the subscription already
// existed (ON CONFLICT DO NOTHING returns no row).
func (r *PluginEventSubscriptionsRepository) Subscribe(
	ctx context.Context,
	arg CreateSubscriptionParams,
) (gen.PluginEventSubscription, error) {
	return r.q.CreatePluginEventSubscription(ctx, gen.CreatePluginEventSubscriptionParams{
		TenantID:     arg.TenantID,
		PluginID:     arg.PluginID,
		TopicPattern: arg.TopicPattern,
		Handler:      arg.Handler,
	})
}

// Unsubscribe removes a single subscription. Missing rows are a no-op.
func (r *PluginEventSubscriptionsRepository) Unsubscribe(
	ctx context.Context,
	pluginID uuid.UUID,
	topicPattern, handler string,
) error {
	return r.q.DeletePluginEventSubscription(ctx, gen.DeletePluginEventSubscriptionParams{
		PluginID: pluginID, TopicPattern: topicPattern, Handler: handler,
	})
}

// ListForPlugin returns every subscription held by the plugin. Used by the
// admin detail UI; the bus uses ListAll and filters in-memory.
func (r *PluginEventSubscriptionsRepository) ListForPlugin(
	ctx context.Context, pluginID uuid.UUID,
) ([]gen.PluginEventSubscription, error) {
	return r.q.ListPluginEventSubscriptions(ctx, pluginID)
}

// ListAll returns every subscription across every plugin. The bus calls
// this at emit time and runs the topic-pattern matcher in-memory.
func (r *PluginEventSubscriptionsRepository) ListAll(
	ctx context.Context,
) ([]gen.PluginEventSubscription, error) {
	return r.q.ListAllPluginEventSubscriptions(ctx)
}

// DeleteAllForPlugin removes every subscription held by the plugin.
// Returns the count removed. CASCADE on plugin_id already covers plugin
// deletes; this covers the "admin force-unsubscribe" path.
func (r *PluginEventSubscriptionsRepository) DeleteAllForPlugin(
	ctx context.Context, pluginID uuid.UUID,
) (int64, error) {
	return r.q.DeletePluginEventSubscriptionsForPlugin(ctx, pluginID)
}
