-- Agent chat module (WS-31). Conversations, messages, tool calls, provider
-- configs, and the per-tenant policy. Every query is scoped by the tenant id
-- pulled from the request context by the repository wrapper (WithTenant),
-- and most are additionally scoped by user_id (chat history is personal).
-- Callers never pass tenant_id directly.

-- name: CreateAgentConversation :one
INSERT INTO agent_conversations (tenant_id, user_id, title, status)
VALUES ($1, $2, $3, 'active')
RETURNING *;

-- name: GetAgentConversationByID :one
SELECT * FROM agent_conversations
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: ListAgentConversations :many
SELECT * FROM agent_conversations
WHERE tenant_id = $1 AND user_id = $2
ORDER BY updated_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAgentConversations :one
SELECT count(*) FROM agent_conversations
WHERE tenant_id = $1 AND user_id = $2;

-- name: TouchAgentConversation :exec
UPDATE agent_conversations
SET updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: SetAgentConversationTitle :exec
UPDATE agent_conversations
SET title = $4, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: SetAgentConversationStatus :exec
UPDATE agent_conversations
SET status = $4, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: DeleteAgentConversation :exec
DELETE FROM agent_conversations
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: CreateAgentMessage :one
INSERT INTO agent_messages (conversation_id, tenant_id, user_id, role, content)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAgentMessages :many
SELECT * FROM agent_messages
WHERE tenant_id = $1 AND conversation_id = $2
ORDER BY created_at ASC;

-- name: SetAgentMessageContent :exec
-- Finalizes a streamed assistant message with the concatenated tokens.
UPDATE agent_messages
SET content = $3
WHERE tenant_id = $1 AND id = $2;

-- name: CountAgentMessagesSince :one
-- Rate-limit window counter: how many user-role messages the caller has
-- sent in the current window. Used by the policy enforcement path.
SELECT count(*) FROM agent_messages
WHERE tenant_id = $1 AND user_id = $2 AND role = 'user' AND created_at > $3;

-- name: CreateAgentToolCall :one
INSERT INTO agent_tool_calls (
    message_id, tenant_id, user_id, tool_name, args,
    requires_confirmation, status
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAgentToolCallByID :one
SELECT * FROM agent_tool_calls
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: ListAgentToolCallsForMessage :many
SELECT * FROM agent_tool_calls
WHERE tenant_id = $1 AND message_id = $2
ORDER BY created_at ASC;

-- name: SetAgentToolCallStatus :exec
-- Transitions a tool call to approved / rejected (HITL) without a result.
UPDATE agent_tool_calls
SET status = $4, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: SetAgentToolCallResult :exec
-- Records the executor's result + flips status to executed (or failed when
-- the caller updates the row itself first).
UPDATE agent_tool_calls
SET result = $4, status = $5, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: CreateAgentProviderConfig :one
INSERT INTO agent_provider_configs (
    tenant_id, user_id, provider, model, base_url, api_key_encrypted
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAgentProviderConfig :one
SELECT * FROM agent_provider_configs
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: ListAgentProviderConfigs :many
SELECT * FROM agent_provider_configs
WHERE tenant_id = $1 AND user_id = $2
ORDER BY created_at ASC;

-- name: DeleteAgentProviderConfig :exec
DELETE FROM agent_provider_configs
WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: GetAgentPolicy :one
-- Returns the tenant's policy row, or no rows when none has been set (the
-- service treats that as the permissive default).
SELECT * FROM agent_policy WHERE tenant_id = $1;

-- name: UpsertAgentPolicy :one
-- Idempotent upsert: exactly one policy row per tenant (unique on tenant_id).
INSERT INTO agent_policy (
    tenant_id, allow_models, force_admin_models, max_messages_per_window,
    window_seconds, spend_cap_credits, deny_tools
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id) DO UPDATE SET
    allow_models            = EXCLUDED.allow_models,
    force_admin_models      = EXCLUDED.force_admin_models,
    max_messages_per_window = EXCLUDED.max_messages_per_window,
    window_seconds          = EXCLUDED.window_seconds,
    spend_cap_credits       = EXCLUDED.spend_cap_credits,
    deny_tools              = EXCLUDED.deny_tools,
    updated_at              = now()
RETURNING *;
