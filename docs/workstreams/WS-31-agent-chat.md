# WS-31 · AI Agent Chat (OpenCode-powered)

```
Status: in-progress
Phase: 5 (cross-cutting: backend agent module + frontend chat UI)
Depends on: WS-05, WS-08, WS-14, WS-15, WS-16, WS-17, WS-18, WS-20
Unblocks: — (future: agent-driven automations/scheduled runs)
```

## Goal

Give every Lahijan user a **natural-language interface to their cloud**: a chat
where they can say *"create a 2-CPU Ubuntu instance in the dev tenant and point
api.example.com at it"* and the agent does it — scoped **exactly** to what that
user is allowed to do, with a confirmation step before anything destructive.

The agent is not a privileged super-user. It is an authorized actor that
inherits the calling user's RBAC permissions and tenant context, so the same
rules that gate the dashboard gate the agent. This makes Lahijan faster for
power users, more accessible to newcomers, and showcases the permission model:
the agent is "just another caller" of the same module services.

The heavy lifting (agent loop, tool orchestration, LLM streaming, model
providers) is delegated to the **OpenCode** harness via the
[opencode-sdk-go](https://github.com/anomalyco/opencode-sdk-go) client.
Lahijan owns: the chat UI, conversation persistence, the **MCP tool bridge**
that exposes Lahijan actions to the agent, provider/key management with admin
limits, token metering, and human-in-the-loop confirmation.

## Scope

**In scope:**

- **Agent chat module (backend)** under `internal/app/lahijan/agent/`:
  conversation + message + tool-call persistence (per-user, per-tenant),
  streaming responses over SSE, lifecycle management of the shared OpenCode
  daemon, and an `opencode-sdk-go` client wrapper.
- **MCP tool bridge** — a Lahijan-served MCP server that exposes the in-scope
  module tools (compute / DNS / object storage / billing-usage / audit) to the
  OpenCode agent. Every tool call enforces `RequirePerm` + tenant scoping at
  the repository layer and emits audit events.
- **Provider configuration** — admin-managed shared providers (optional
  fallback) **plus** user-supplied keys (BYOK). All API keys stored AES-GCM
  encrypted at rest (per security conventions). Provider/model resolution =
  `(admin providers ∪ user providers) ∩ admin allowlist`.
- **Admin controls (all five):** provider/model allowlist, per-user rate
  limits, token / spend caps, per-platform "force admin models" (disable BYOK)
  toggle, and an extra tool denylist (restrict which Lahijan tools the agent
  may invoke beyond normal RBAC).
- **Token metering & billing** — when an admin-provided model is used, meter
  token usage into the existing `usage_events` + ledger (WS-17) and debit the
  user's balance. BYOK usage is **not** charged. Spend caps are checked against
  the ledger.
- **Human-in-the-loop (HITL) confirmation** — tool calls are classified
  read vs. write/destructive. Write/destructive calls enter a `pending` state
  and require an explicit UI confirm/decline before execution.
- **Audit** — every privileged agent tool call emits an audit event before the
  side effect and updates it after, with the agent clearly recorded as the
  actor (on behalf of the user). See Open questions for the actor model.
- **Frontend chat UI** in `web/` — conversation sidebar, streaming message
  list, tool-call cards, confirmation dialogs, user provider settings, and an
  admin agent-settings page. All strings via `t()`; RTL-safe; `usePerm` gates
  privileged triggers.
- **Config + deploy** — new `agent` / `opencode` section in the Viper config;
  add the OpenCode daemon to the dev and prod compose stacks.

**Out of scope** (link to future work):

- Agent-initiated background jobs / scheduled automations → future WS
- Cross-session agent memory / long-term knowledge base → future WS
- Sharing conversations between users
- Voice / image / multimodal input
- User-editable agent personas or system prompts (admin-defined default only
  in v1)
- Replacing the module UIs — chat and the existing dashboards coexist
- Charging for BYOK traffic → explicitly excluded (see billing decision above)

## Required reading for the AI session

A fresh AI session that picks up this WS must read these files first:

- `/AGENTS.md` — project heart (pillars 2, 4, 6, 7, 8, 11, 12 all bear on this WS)
- `docs/architecture/conventions.md` — especially the **HTTP / API**,
  **Security**, **Database**, and **Frontend** sections
- `docs/workstreams/WS-08-rbac-audit-log.md` — RBAC + audit; the agent is a
  permission-gated actor
- `docs/workstreams/WS-17-billing-metering.md` — `usage_events` + ledger; this
  WS reuses them for token metering
- `docs/workstreams/WS-14-compute-module.md` — the compute service surface the
  compute tools wrap (same shape applies to WS-15 DNS / WS-16 storage)
- `docs/glossary.md` — **Actor**, **Permission**, **Usage event** definitions
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`
- `.opencode/skills/frontend-foundations/SKILL.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- `web/AGENTS.md` (frontend area) and `internal/app/lahijan/AGENTS.md` if present
- The SDK README + `api.md` at the [opencode-sdk-go](https://github.com/anomalyco/opencode-sdk-go)
  repository, and the OpenCode docs for its **session**, **event/SSE**, and
  **MCP** support

## Deliverables

### Database (paired, reversible migrations + sqlc)

- `agent_conversations` — `id`, `tenant_id`, `user_id`, `title`, `status`,
  `created_at`, `updated_at`.
- `agent_messages` — `id`, `conversation_id`, `tenant_id`, `user_id`,
  `role` (user / assistant / tool), `content`, `created_at`.
- `agent_tool_calls` — `id`, `message_id`, `tenant_id`, `user_id`,
  `tool_name`, `args_json`, `result_json`, `requires_confirmation`,
  `status` (pending / approved / rejected / executed / failed),
  `created_at`, `updated_at`.
- Provider config storage for admin (global) and user (per-user) scopes,
  encrypted-key columns; exact table split decided in the ADR.
- Admin policy storage: provider/model allowlist, rate-limit + spend-cap
  defaults, force-admin-models flag, tool denylist (with optional per-tenant
  overrides).
- **Reuse** the existing `usage_events` + ledger tables for token metering
  (`resource_type = 'agent_token'`) — no new metering table.

### Backend (`internal/app/lahijan/agent/`)

- `harness/` — OpenCode daemon lifecycle (start/health/reconnect) + the
  `opencode-sdk-go` client wrapper (session create, message send, SSE event
  consumption).
- `mcp/` — the MCP server: one tool per in-scope module action, each calling
  the existing domain services with the user's auth context (RBAC + tenant).
- `providers/` — admin + user provider config service; AES-GCM key encryption;
  effective-provider resolution against the allowlist.
- `limits/` — admin controls enforcement (allowlist, rate limit, spend cap,
  force-admin-models, tool denylist).
- `meter/` — token metering → `usage_events` + ledger debit (admin models only).
- `service/` + `repository/` — conversation/message/tool-call CRUD and the
  HITL confirm/decline state machine.

### HTTP API (under `/api/v1/`, OpenAPI 3.1 updated; clients regenerated)

- `POST/GET /api/v1/agent/conversations` — list + create conversations
- `GET /api/v1/agent/conversations/:id` — conversation + history (paginated)
- `POST /api/v1/agent/conversations/:id/messages` — send message; **SSE**
  stream of assistant tokens / tool-call events
- `POST /api/v1/agent/tool-calls/:id/confirm` — approve or decline a pending
  (destructive) tool call (HITL)
- `GET/POST/PATCH /api/v1/agent/provider-configs` — user BYOK config
- `GET/PATCH /api/v1/admin/agent/policy` — admin allowlist / caps / toggles
- `GET/POST /api/v1/admin/agent/providers` — admin shared providers
- Every error uses the standard error envelope; middleware order unchanged.

### Frontend (`web/`)

- Chat page: conversation sidebar, streaming message list, composer.
- Tool-call cards with live status; confirmation modal for destructive calls.
- User **Provider Settings** page (BYOK keys, model picker, masked secrets).
- Admin **Agent Settings** page (allowlist, rate limits, spend caps,
  force-admin-models toggle, tool denylist).
- TanStack Query for server state; Zustand for active-conversation UI state.

### Config + deploy

- New `agent:` / `opencode:` block in
  `internal/app/lahijan/conf/.lahijan.conf.default.yaml` (daemon path, port,
  MCP endpoint, default limits, encryption key reference).
- OpenCode daemon added to `deployments/docker-compose*.yml` (dev + prod).

### ADRs

- **ADR: Agent / OpenCode integration architecture** — shared daemon model,
  SDK wiring, session ↔ conversation mapping.
- **ADR: MCP tool bridge + RBAC/audit mapping** — how Lahijan tools are
  exposed, permission-gated, and audit-logged; the agent **actor model**
  (new `agent` actor type vs. user-with-flag).
- **ADR: Provider config, secrets & admin limits** — key encryption,
  effective-provider resolution, the five admin controls, and the
  meter-on-admin-models-only billing decision.

## Definition of Done

- [ ] migrations paired up + down, reversible, tested
- [ ] `sqlc generate` clean; queries tenant + user scoped
- [ ] OpenCode daemon lifecycle managed by the backend; auto-reconnects on
      failure; health-checked
- [ ] MCP server exposes at least one tool per in-scope module (compute, DNS,
      storage, billing-usage, audit)
- [ ] every tool call enforces `RequirePerm` + tenant scoping and emits an
      audit event before and after; agent actor recorded
- [ ] destructive tool calls are blocked until the user confirms via
      `/confirm` (HITL); `pending` state observable in the UI
- [ ] provider API keys stored AES-GCM encrypted; never logged (verified
      against the redact slog handler)
- [ ] all five admin controls enforced: allowlist (selection blocked),
      rate-limit (per-window), spend-cap (rejects when exceeded),
      force-admin-models (BYOK disabled), tool denylist (tool hidden/blocked)
- [ ] token usage metered for admin-provided models → `usage_events` +
      ledger debit; BYOK usage unmetered
- [ ] SSE streaming backend→frontend; tokens stream live; no buffering/regressions
- [ ] OpenAPI 3.1 spec updated; Go + TS clients regenerated
- [ ] chat UI complete: conversation list, streaming, tool-call cards,
      confirm modal, user provider settings, admin agent settings
- [ ] all UI strings via `t()`; layout RTL-safe; `usePerm` gates privileged
      triggers; no hardcoded English
- [ ] at least one happy-path + one failure-path test per public function
- [ ] fake OpenCode server (httptest) for integration tests; MCP tool tests
      against fake domain services
- [ ] Playwright e2e: one create-resource-via-agent flow (happy) **and** one
      destructive-action confirmation flow
- [ ] `make lint test` green
- [ ] `docs/workstreams/WS-31-agent-chat.md` Status updated; this WS's row
      added to `docs/workstreams/README.md`
- [ ] PR template checklist ticked

## Open questions

- **Agent actor model** — introduce a new audit `actor_type = "agent"` (with
  the on-behalf-of `user_id`), or reuse `actor_type = "user"` with an
  `via_agent` flag? Decision lands in the RBAC/audit ADR.
- **SDK event hooks** — does the SDK expose tool-call *interception* events so
  Lahijan can implement HITL cleanly, or must HITL be enforced inside the MCP
  tool itself (return a "confirmation required" result)? Needs a short spike
  against the SDK's event stream before locking the design.
- **Module path of the fork** — confirm whether the `anomalyco/opencode-sdk-go`
  fork keeps the upstream `github.com/sst/opencode-sdk-go` module path or has
  its own `github.com/anomalyco/opencode-sdk-go`; pin the version in `go.mod`.
- **MCP transport** — HTTP/SSE (Lahijan already serves HTTP, so natural) vs.
  stdio (OpenCode spawns the MCP server as a subprocess). Confirm in the ADR.
- **Spend-cap units** — token count vs. credits vs. USD-equivalent; align with
  the WS-17 ledger currency model.
- **Rate-limit storage** — in-memory (single-node only) vs. Postgres-backed
  (multi-node-ready per ADR-0009). Default Postgres for cluster readiness.

## Notes

### Implementation progress (first slice — branch `feat/ws-31-agent-chat`)

Landed and green (`go build`, `golangci-lint`, `go test`, `web typecheck`,
`web lint`, `web test`):

- **RBAC** — 7 `agent.*` permissions + grants across the role bundles.
- **Audit** — agent action constants + emission on every privileged call.
- **DB** — migration `0049_agent_chat` (conversations, messages, tool_calls,
  provider_configs, policy) + sqlc queries + `AgentRepository` + `Repos` wiring.
- **Domain** — `internal/app/lahijan/agent/`: `Service` (conversation CRUD,
  streamed turn, HITL confirm state machine, BYOK provider keys via AES-GCM,
  tenant policy CRUD + enforcement: rate cap, force-admin-models, allowlist,
  tool denylist) behind two seams:
  - `Harness` — satisfied by `StubHarness` today.
  - `ToolExecutor` — satisfied by `StubToolExecutor` (`ping` + a destructive
    `sample.destructive` so the HITL flow is demonstrable).
  13 unit tests cover CRUD, the streamed turn, HITL approve/decline, and every
  policy enforcement branch.
- **HTTP** — OpenAPI `/api/v1/agent/*` paths + schemas, codegen regenerated;
  thin handlers (`api/agent_handlers.go`) incl. SSE send-message; `AuditGate`
  path→slug mapping; `Server`/`ServerDeps` wiring; `program` wiring
  (`agent_module.go`, opt-in via `conf.agent.enabled`).
- **i18n** — backend `en`/`fa` keys + frontend `en`/`fa` keys.
- **Frontend** — `web/src/features/agent/` (typed API hooks + SSE streamer),
  `AgentChat` (conversation list + streaming message view + composer + HITL
  confirm modal), `/agent` route, sidebar nav entry. Degrades to a "not
  enabled" panel on backend 501.

### Implementation progress (second slice — branch `feat/ws-31-agent-chat`)

Makes the agent **actually call tools and answer from live data** with BYOK
providers, without waiting for the OpenCode daemon (WS-31a). Green: `go build`,
`golangci-lint`, `go test`, `web typecheck`, `web lint`, `web test`.

- **`agent/llm_harness.go`** — the `LLMHarness` now advertises the turn's tools
  to the model (OpenAI `tools` + `tool_choice`), reassembles streamed
  `tool_calls` deltas, and runs a bounded (≤6-round) read-only agent loop:
  when the model requests a **non-destructive** tool the harness executes it,
  feeds the result back as a proper `tool` message, and re-prompts so the
  answer is grounded in live data. **Destructive** tools are surfaced as
  `EventToolCall` (no result) for the existing HITL confirm flow. New
  `WithToolExecutor`; the system prompt now tells the model to *call* tools
  instead of pointing the user at the dashboard.
- **`agent/module_tools.go`** — `ModuleToolBridge`, the first real
  `ToolExecutor`: read-only `dns.list_zones` / `compute.list_instances` /
  `storage.list_buckets` over the shared repos (tenant-scoped at the repo
  seam), projecting rows to clean JSON. All `Destructive()` false.
- **`agent/harness.go`** — `ToolDescriptor` gains a `Parameters` JSON Schema
  field so tools advertise real argument schemas.
- **`agent/service.go`** — `handleToolCall` now recognises harness-executed
  results (`EventToolCall` carrying `Result`) and persists + forwards them
  without re-executing.
- **`program/agent_module.go`** — wires `ModuleToolBridge` into both
  `Deps.Tools` and the harness, replacing `StubToolExecutor`.
- **`agent/llm_harness_test.go`** — covers tool advertisement, the read-only
  loop, destructive HITL surfacing, the no-provider path, and `parseLimit`.

> **Architectural divergence recorded in ADR-0041.** This slice talks to the
> model over a plain OpenAI-compatible `/chat/completions` client instead of
> the shared OpenCode daemon the brief specifies. It satisfies the same
> `agent.Harness` seam, so the daemon path (WS-31a) remains a clean future
> swap; the decision is captured in ADR-0041.

### Implementation progress (third slice — WS-31b + WS-31c, branch `feat/ws-31b-tool-bridge`)

Closes the read-only tool-bridge + admin-limits/metering gaps. Green: `go
build`, `golangci-lint`, `go test`.

**WS-31b — tool bridge + RBAC/audit (ADR-0042):**
- `ModuleToolBridge` now exposes **one tool per in-scope module** — added
  `billing.list_usage` + `audit.list_events` alongside compute/DNS/storage.
- `EnforcingExecutor` decorates the bridge: every call runs `rbac.Require`
  against the calling user's permissions and emits an audit row before +
  after (`audit.ActionAgentToolExecute`, `metadata.via_agent = true`). The
  actor is `actor_type = "user"` (no schema change); `WithActorUserID`
  threads the identity through the turn.
- Wired as both `Deps.Tools` and the harness executor, so the read-only
  loop, the inline path, and the HITL confirm path are gated identically.

**WS-31c — metering + spend cap + admin limits (ADR-0043):**
- Token usage captured from the stream (`stream_options.include_usage`) and
  carried on `EventDone.Usage`.
- `agent.Meter` seam + `program/agent_meter.go` adapter: an admin-provided
  turn records a `usage_events` row (`agent_token`) and debits the ledger
  via the canonical `billing.Service.PostCharge` (idempotent on
  `agent:conv:<id>:msg:<id>`). BYOK turns are unmetered.
- Spend cap enforced pre-send (`enforceSpendCap`): a tenant with
  `SpendCapCredits > 0` and balance `<= 0` is refused (`ErrSpendCap`).
- Interim price knob `agent.billing.centsPer1kTokens` (default 2).
- **ADRs 0041 / 0042 / 0043 written** + indexed.

**Still deferred (the remaining DoD items):**
- Destructive module tools (create/delete via the domain services) — the
  HITL infra + the enforcer already cover the path; the tools themselves
  need service wiring + fakes.
- Admin-shared (platform-level) providers + the `force_admin_models` real
  fallback — needs a `0050_agent_admin_providers` migration + sqlc regen;
  today the toggle disables BYOK and the agent has nothing to run on.
- Playwright e2e for one create-via-agent + one destructive-confirm flow.

Deferred to the follow-on sub-streams (the seams above are the swap points):

- **WS-31a** — real OpenCode harness via `opencode-sdk-go` (replace
  `LLMHarness`) + the shared-daemon lifecycle. *(The OpenAI-compatible
  `LLMHarness` above is an interim that makes BYOK tool-calling work today; it
  does not satisfy the daemon-lifecycle DoD item.)*
- **WS-31b** — MCP tool bridge mapping the compute/DNS/storage/billing/audit
  module actions to agent tools (replace `ModuleToolBridge`). *(Partially done:
  three read-only list tools exist. Still missing: billing-usage + audit tools,
  all create/delete/modify (destructive) tools, a true MCP server transport,
  and per-call `RequirePerm` + audit emission.)*
- **WS-31c** — token metering into the WS-17 ledger (admin-provided models
  only) + spend-cap enforcement; admin shared providers (platform-level) +
  the force-admin-models real path; admin agent-settings UI; Playwright e2e
  for one create-via-agent + one destructive-confirm flow; the remaining
  ADRs (MCP bridge + actor model + provider/limits).

### Architectural sketch

```
Dashboard (web/)
   │  SSE  (POST /agent/conversations/:id/messages)
   ▼
Agent HTTP handlers (thin) ──► audit, RBAC, tenant ctx
   │  opencode-sdk-go (HTTP client)
   ▼
Shared OpenCode daemon (managed by backend, localhost)
   │  MCP tool calls
   ▼
Lahijan MCP server (HTTP/SSE endpoint)
   │  calls with the user's auth context
   ▼
Domain services: compute / dns / storage / billing / audit
```

### Effective-provider resolution

```
candidates = (admin_providers if enabled else ∅)
           ∪ (user_providers if BYOK_allowed else ∅)
allowed    = candidates ∩ admin_allowlist
charge?    = provider ∈ admin_providers   (BYOK → unmetered)
```

### Limit checkpoints

| Control             | Checked at                          |
|---------------------|-------------------------------------|
| provider allowlist  | provider/model selection            |
| rate limit          | message send (per user, per window) |
| spend cap           | pre-send (ledger balance) + meter   |
| force-admin-models  | provider resolution                 |
| tool denylist       | pre-tool-call (before RBAC even)    |

### Size warning

This WS is large (backend module + MCP bridge + provider/limits/metering +
full chat UI). If implementation sprawls, split it along the existing
letter-suffix pattern (`WS-31a` backend + MCP, `WS-31b` provider/limits/
metering, `WS-31c` frontend) without rewriting this brief.
