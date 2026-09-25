---
title: Agent policy
description: Turn the Agent on or off for the server and set per-tenant limits on models, message rate and the lookups it may use.
---

The [Agent](/docs/agent/overview) is the chat assistant in the dashboard. Operators decide whether it runs at all; tenant administrators set a policy that limits how it is used in their tenant. This page covers both.

## Server settings

| Key                              | Default            | Purpose                                                                                                                                                            |
| -------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `agent.enabled`                  | `true`             | Turns the Agent on. When `false`, every `/api/v1/agent/*` endpoint answers `501` and the dashboard shows "The agent chat assistant is not enabled on this server." |
| `agent.defaultConversationTitle` | `New conversation` | Title for conversations started without one.                                                                                                                       |
| `agent.maxMessageBytes`          | `65536`            | Largest message a user can send, in bytes. Longer messages are rejected with `400`.                                                                                |
| `agent.billing.centsPer1kTokens` | `2`                | Price for turns on operator-provided models. Has no effect yet (see [Spend cap](#spend-cap)).                                                                      |

Override them with environment variables named after the key, for example `LAHIJAN_AGENT_ENABLED=false`. See [Configuration](/docs/reference/configuration).

Users bring their own model-provider keys. Lahijan encrypts each key with `auth.secrets.encryptionKey` (a base64-encoded 32-byte key) before storing it. Outside the `dev` environment, if that key is empty or invalid, saving a provider fails with an internal error. In `dev` an insecure built-in key is used instead.

The Agent calls the provider's API from the Lahijan server, so the server needs outbound network access to the providers your users choose, or to the local model server they point at.

## Who can change the policy

The policy belongs to a tenant. Changing it needs `agent.policy.manage`, held by `tenant.owner`, `tenant.admin` and `platform.admin`. Anyone who can read Agent conversations (every tenant role) can read the policy, so members see the limits that apply to them.

In the dashboard the page is **Administration > Agent Policy**. The **Administration** section of the sidebar is only shown to platform administrators; tenant owners and administrators can open the same page directly at `/admin/agent`.

## The policy fields

| Dashboard field                                     | API field              | Default | Effect                                                       |
| --------------------------------------------------- | ---------------------- | ------- | ------------------------------------------------------------ |
| **Allowed models**                                  | `allowModels`          | empty   | Models users may run. Empty allows all.                      |
| **Max messages per window**                         | `maxMessagesPerWindow` | `0`     | Messages each user may send per window. `0` means unlimited. |
| **Window seconds**                                  | `windowSeconds`        | `60`    | Length of the sliding window, in seconds.                    |
| **Spend cap (credits)**                             | `spendCapCredits`      | `0`     | See [Spend cap](#spend-cap).                                 |
| **Denied tools**                                    | `denyTools`            | empty   | Lookups the Agent may not use.                               |
| **Force admin-provided models (disable user keys)** | `forceAdminModels`     | off     | Disables users' own keys.                                    |

Until someone saves a policy, the tenant uses these defaults.

### Allowed models

Enter model names separated by commas. Matching ignores case. A name ending in `*` matches every model that starts with the rest, so `gpt-4o*` allows `gpt-4o` and `gpt-4o-mini`. Any other name must match exactly; other wildcard patterns are not supported.

When a user sends a message, the Agent picks the first of their provider configurations whose model is on the list. If none matches, the Agent replies that no model provider is configured. A configuration saved without a model name never matches a non-empty list, so ask users to fill in **Model** when you use an allowlist.

### Message rate

The limit is per user. Lahijan counts the messages the user sent in the last `windowSeconds` seconds; once the count reaches `maxMessagesPerWindow`, new messages are refused with `429` until older ones fall out of the window.

### Denied tools

Enter tool names separated by commas; matching is exact and ignores case. A denied tool is not offered to the model, and if the model asks for it anyway the call is refused. The tools available today are all read-only:

| Tool                     | Reads                             |
| ------------------------ | --------------------------------- |
| `compute.list_instances` | Instances in the tenant           |
| `dns.list_zones`         | DNS zones in the tenant           |
| `storage.list_buckets`   | Buckets in the tenant             |
| `billing.list_usage`     | The user's own usage records      |
| `audit.list_events`      | Recent audit events in the tenant |

Each tool is also checked against the user's own permissions when it runs, so denying a tool is only needed when you want to keep that data away from the model provider entirely.

### Force admin-provided models

This switch turns off users' own provider keys so that only operator-provided models can be used. Operator-provided models are not available in this release, so turning it on stops the Agent from answering anyone in the tenant: sending a message fails with `403`.

### Spend cap

The spend cap applies only to turns on operator-provided models, which would be charged to the user's balance. Turns on a user's own key are never charged. Since operator-provided models do not exist yet, the spend cap currently has no effect.

## Set the policy with the API

`PUT /api/v1/agent/policy` replaces the whole policy, so send every field:

```sh
curl -X PUT https://cloud.example.com/api/v1/agent/policy \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $LAHIJAN_TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{
        "allowModels": ["gpt-4o*", "llama3*"],
        "forceAdminModels": false,
        "maxMessagesPerWindow": 30,
        "windowSeconds": 3600,
        "spendCapCredits": 0,
        "denyTools": ["audit.list_events"]
      }'
```

A `windowSeconds` of `0` or less is stored as `60`. `GET /api/v1/agent/policy` returns the current policy, or the defaults if none was saved.

Each change is written to the audit log as `agent.policy.update`, with the new force-admin-models and denied-tools values. Like other Agent events, it is stored without a tenant and does not appear on the tenant **Audit Log** page in this release.
