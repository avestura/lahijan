-- plugin_event_subscriptions (WS-10b). The event bus consults this table
-- at emit time to find the match list for a topic; subscriptions persist
-- across process restarts so events emitted while the plugin is disabled
-- are queued and delivered on resume.

-- name: CreatePluginEventSubscription :one
-- Idempotent on (plugin_id, topic_pattern, handler).
INSERT INTO plugin_event_subscriptions (tenant_id, plugin_id, topic_pattern, handler)
VALUES ($1, $2, $3, $4)
ON CONFLICT (plugin_id, topic_pattern, handler) DO NOTHING
RETURNING *;

-- name: DeletePluginEventSubscription :exec
-- Remove a single subscription. Missing rows are a no-op.
DELETE FROM plugin_event_subscriptions
WHERE plugin_id = $1 AND topic_pattern = $2 AND handler = $3;

-- name: ListPluginEventSubscriptions :many
-- Every subscription held by the plugin. The bus uses this to drive
-- the per-plugin dispatch path; the admin detail UI uses it to show
-- what the plugin is listening to.
SELECT * FROM plugin_event_subscriptions
WHERE plugin_id = $1
ORDER BY created_at;

-- name: ListAllPluginEventSubscriptions :many
-- Every subscription across every plugin. The bus uses this at emit
-- time to find every plugin that matches the topic; the per-plugin
-- filter then enqueues the dispatch.
SELECT * FROM plugin_event_subscriptions
ORDER BY created_at;

-- name: DeletePluginEventSubscriptionsForPlugin :execrows
-- Used when a plugin is uninstalled so its subscriptions do not
-- linger. CASCADE on plugin_id already covers plugin deletes; this
-- query covers the "admin force-unsubscribe" path.
DELETE FROM plugin_event_subscriptions
WHERE plugin_id = $1;
