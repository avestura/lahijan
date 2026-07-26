-- 0049_agent_chat.down: reverse of 0049_agent_chat.up.
-- Drop in reverse dependency order: tool_calls -> messages -> conversations;
-- provider_configs + policy are independent of the conversation tree.

DROP TABLE IF EXISTS agent_tool_calls;
DROP TABLE IF EXISTS agent_provider_configs;
DROP TABLE IF EXISTS agent_policy;
DROP TABLE IF EXISTS agent_messages;
DROP TABLE IF EXISTS agent_conversations;
