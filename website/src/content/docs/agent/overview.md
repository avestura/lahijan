---
title: Agent
description: The chat assistant in the dashboard that answers questions about your resources using your own model-provider key and your own permissions.
---

The Agent is a chat panel in the dashboard. You type a question in plain language, for example "Which instances do I have?" or "What happened in the audit log today?", and it answers by looking up your resources and summarising them. It works on your behalf: it can only see what your role in the current tenant lets you see.

In the current release the Agent can only **read**. It cannot create, change or delete anything.

## Before you start

- Your operator must have the Agent turned on. If not, the page shows "The agent chat assistant is not enabled on this server."
- You need an API key from a model provider (the service that runs the language model). Lahijan does not supply one.
- Your role needs the `agent.message.send` permission to send messages. Tenant owners, administrators and members have it; viewers can read past conversations but not send.

## Add your model provider

1. Open **Settings > AI Provider**.
2. Under **Add a provider**, choose a **Provider**.
3. Optionally enter a **Model**. If you leave it empty, `gpt-4o-mini` is used.
4. Optionally enter a **Base URL**. Leave it blank to use the provider's default address; set it for a self-hosted server or a proxy.
5. Paste your **API key** and select **Add provider**.

The provider list contains services that offer an OpenAI-compatible chat API: `openai`, `openrouter`, `302ai`, `groq`, `together`, `deepseek`, `cerebras`, `deepinfra`, `fireworks`, `moonshot`, `minimax`, `nvidia`, `venice`, `xai`, `zai`, `zai-coding-plan`, and the local servers `ollama`, `lmstudio` and `llamacpp`. Providers with a different API are not supported.

Your key is encrypted before it is stored and is never sent back to the browser; the list only shows "key saved". You can add one configuration per provider in each tenant. To replace a key, delete the provider with **Delete provider** and add it again.

When you send a message, the Agent uses the first enabled provider in your list whose model is allowed by the tenant's policy. If none qualifies, it replies "No model provider is configured. Add an API key in Agent Settings (under Settings)."

> [!WARNING]
> Your messages, the recent conversation history and the data the Agent looks up (names and states of your instances, zones, buckets, usage and audit events) are sent to the provider you chose. Pick a provider whose data handling you accept, or run a local model with `ollama`, `lmstudio` or `llamacpp`.

## Chat with the Agent

1. Open **Agent** in the sidebar.
2. Select **New conversation**.
3. Type your question in the message box and send it.

The reply appears as it is written; "Thinking…" is shown until the first words arrive. The lookups the Agent ran are saved with the conversation and returned by the API, but the chat panel shows only the text of the reply.

Conversations are listed on the left. They are private: only you can see your conversations, and only in the tenant where you started them. Select **Delete conversation** to remove one and its whole history.

Each message can be up to 64 KiB (`agent.maxMessageBytes`). The last 20 messages of a conversation are sent to the model as context.

## What the Agent can look up

| Lookup                   | What it returns                   | Permission it needs     |
| ------------------------ | --------------------------------- | ----------------------- |
| `compute.list_instances` | Your tenant's instances           | `compute.instance.read` |
| `dns.list_zones`         | Your tenant's DNS zones           | `dns.zone.read`         |
| `storage.list_buckets`   | Your tenant's buckets             | `s3.bucket.read`        |
| `billing.list_usage`     | Your own usage records            | `billing.ledger.read`   |
| `audit.list_events`      | Recent audit events in the tenant | `audit.read`            |

Each lookup returns at most 50 rows. Every lookup is checked against your permissions at the moment it runs, exactly as if you had opened the page yourself; if you lack the permission, the lookup fails and the Agent tells you. A single reply can make up to six rounds of lookups.

A tenant administrator can hide lookups from the Agent with the policy's denied-tools list.

## Approvals

When a future release adds actions that change resources, the Agent will pause before running one and show a **Confirm action** dialog with **Approve** and **Decline**. Nothing runs until you approve. No such actions exist yet, so you will not see this dialog today.

## Limits set by your tenant

A tenant administrator can set a policy for everyone in the tenant (see [Agent policy](/docs/admin/agent-policy)):

- **Rate limit**: a maximum number of messages per time window. When you reach it, sending fails with "rate limited" (`429`) until the window passes.
- **Allowed models**: only provider configurations with a matching model are used.
- **Denied tools**: lookups the Agent is not offered.
- **Force admin-provided models**: turns off personal provider keys. Operator-provided models do not exist yet, so while this is on the Agent cannot run at all (`403`).

## Cost

Using your own provider key costs you nothing in Lahijan: the provider bills you directly and nothing is written to your Lahijan ledger. The policy's spend cap only applies to operator-provided models, which are not available in this release.

## What is recorded

The Agent writes these actions to the audit log: `agent.conversation.create`, `agent.conversation.delete`, `agent.message.send`, `agent.tool.execute` (one row per lookup, including the permission checked and whether it was denied), `agent.provider.save` and `agent.provider.delete`. Rows are marked as coming from the Agent.

> [!NOTE]
> These rows are currently stored without a tenant, so they do not appear on the tenant **Audit Log** page or in its export.

## Use the API

All endpoints need a tenant header (see [Access tokens](/docs/account/access-tokens#tenant-header)).

| Method and path                                              | Purpose                                                        |
| ------------------------------------------------------------ | -------------------------------------------------------------- |
| `GET /api/v1/agent/conversations`                            | List your conversations.                                       |
| `POST /api/v1/agent/conversations`                           | Start one. Optional body: `{"title": "..."}`.                  |
| `GET /api/v1/agent/conversations/{conversationId}`           | A conversation with its messages and lookups.                  |
| `DELETE /api/v1/agent/conversations/{conversationId}`        | Delete a conversation.                                         |
| `POST /api/v1/agent/conversations/{conversationId}/messages` | Send `{"message": "..."}` and receive the reply as a stream.   |
| `POST /api/v1/agent/tool-calls/{toolCallId}/confirm`         | Approve or decline a pending action with `{"approved": true}`. |
| `GET /api/v1/agent/providers`                                | List your provider configurations (keys never returned).       |
| `POST /api/v1/agent/providers`                               | Add one: `provider`, `apiKey`, optional `model` and `baseUrl`. |
| `DELETE /api/v1/agent/providers/{providerId}`                | Remove one.                                                    |
| `GET /api/v1/agent/policy`                                   | Read the tenant's limits.                                      |

The message endpoint answers with Server-Sent Events. Each event is a line `data: <json>` where `type` is `text` (a piece of the reply), `tool_call`, `tool_result`, `done` or `error`.

```sh
curl -N -X POST https://cloud.example.com/api/v1/agent/conversations/<conversation id>/messages \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"message": "List my DNS zones"}'
```

```console
$ curl -N ...
data: {"type":"tool_result","tool":"dns.list_zones","tool_call_id":"...","result":[...]}
data: {"type":"text","text":"You have two zones: "}
data: {"type":"text","text":"example.com and example.org."}
data: {"type":"done"}
```
