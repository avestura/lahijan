-- 0049_agent_chat: persistence for the AI agent chat module (WS-31).
--
-- The agent chat module lets users drive Lahijan through natural language.
-- The OpenCode harness runs the agent loop; Lahijan owns the conversation
-- history, the per-tool-call human-in-the-loop state machine, the per-user
-- model-provider keys (BYOK, AES-GCM encrypted), and the per-tenant agent
-- policy (allowlist / rate / spend / force-admin-models / tool denylist).
--
-- Per ADR-0002 every table here is tenant-scoped (tenant_id NOT NULL) and
-- carries the standard audit columns. Conversations, messages, and tool
-- calls are additionally user-scoped (user_id NOT NULL) because chat
-- history is personal; provider configs are per (tenant, user) because a
-- user's BYOK key is resolved within the active tenant context (matches
-- the repository-layer tenant scoping every other module uses).

CREATE TABLE agent_conversations (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id     UUID        NOT NULL,
    title       TEXT        NOT NULL DEFAULT '',
    -- active (user can send) | archived (read-only history).
    status      TEXT        NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agent_conversations_tenant_user ON agent_conversations (tenant_id, user_id);
ALTER TABLE agent_conversations ADD CONSTRAINT agent_conversations_status_valid
    CHECK (status IN ('active', 'archived'));

CREATE TABLE agent_messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id         UUID        NOT NULL,
    -- role: who produced the message. 'user' = the human prompt, 'assistant'
    -- = the agent reply (streamed then finalized), 'tool' = a tool result
    -- surfaced inline (also tracked in agent_tool_calls for the HITL trail).
    role            TEXT        NOT NULL,
    content         TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agent_messages_conversation ON agent_messages (conversation_id, created_at);
ALTER TABLE agent_messages ADD CONSTRAINT agent_messages_role_valid
    CHECK (role IN ('user', 'assistant', 'tool'));

CREATE TABLE agent_tool_calls (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id            UUID        NOT NULL REFERENCES agent_messages (id) ON DELETE CASCADE,
    tenant_id             UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id               UUID        NOT NULL,
    tool_name             TEXT        NOT NULL,
    args                  JSONB       NOT NULL DEFAULT '{}'::jsonb,
    result                JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Destructive (write-class) calls set requires_confirmation = true; the
    -- service parks them in status = 'pending' until the user approves via
    -- the /agent/tool-calls/{id}/confirm endpoint (human-in-the-loop).
    requires_confirmation BOOLEAN     NOT NULL DEFAULT FALSE,
    -- pending | approved | rejected | executed | failed.
    status                TEXT        NOT NULL DEFAULT 'executed',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agent_tool_calls_message   ON agent_tool_calls (message_id);
CREATE INDEX idx_agent_tool_calls_tenant    ON agent_tool_calls (tenant_id, user_id);
ALTER TABLE agent_tool_calls ADD CONSTRAINT agent_tool_calls_status_valid
    CHECK (status IN ('pending', 'approved', 'rejected', 'executed', 'failed'));

-- agent_provider_configs: per-user BYOK model-provider keys. The
-- api_key_encrypted blob is AES-256-GCM ciphertext produced by the
-- auth/secrets.Crypto envelope (same key as IdP tokens / backup targets);
-- the raw key is NEVER stored or logged. Scoped to (tenant_id, user_id)
-- so resolution reads within the tenant context like every other module.
CREATE TABLE agent_provider_configs (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id            UUID        NOT NULL,
    provider           TEXT        NOT NULL,
    model              TEXT        NOT NULL DEFAULT '',
    base_url           TEXT        NOT NULL DEFAULT '',
    api_key_encrypted  BYTEA       NOT NULL,
    enabled            BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- One config per (tenant, user, provider): a user picks among their
-- configured providers per conversation, not per model.
CREATE UNIQUE INDEX uq_agent_provider_tenant_user_provider
    ON agent_provider_configs (tenant_id, user_id, provider);

-- agent_policy: one row per tenant. The tenant admin edits it via
-- /api/v1/agent/policy (agent.policy.manage). force_admin_models disables
-- BYOK (only operator-provided models are usable); max_messages_per_window +
-- window_seconds form a sliding-window rate limit (0 = unlimited);
-- spend_cap_credits caps total agent spend (0 = unlimited, enforced against
-- the WS-17 ledger when admin-provided models are wired in a follow-on);
-- allow_models + deny_tools are glob patterns / tool names.
CREATE TABLE agent_policy (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    allow_models              TEXT[]      NOT NULL DEFAULT '{}',
    force_admin_models        BOOLEAN     NOT NULL DEFAULT FALSE,
    max_messages_per_window   INTEGER     NOT NULL DEFAULT 0,
    window_seconds            INTEGER     NOT NULL DEFAULT 60,
    spend_cap_credits         BIGINT      NOT NULL DEFAULT 0,
    deny_tools                TEXT[]      NOT NULL DEFAULT '{}',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_agent_policy_tenant ON agent_policy (tenant_id);

COMMENT ON TABLE agent_conversations     IS 'Per-user agent chat conversations (WS-31).';
COMMENT ON TABLE agent_messages          IS 'Agent chat messages: user prompts, assistant replies, tool results (WS-31).';
COMMENT ON TABLE agent_tool_calls        IS 'Per-message agent tool invocations + HITL confirmation state (WS-31).';
COMMENT ON TABLE agent_provider_configs  IS 'Per-user BYOK model-provider keys; api_key_encrypted is AES-256-GCM ciphertext (WS-31).';
COMMENT ON TABLE agent_policy            IS 'Per-tenant agent policy: allowlist, rate, spend cap, force-admin-models, tool denylist (WS-31).';
