# ADR-0042: Agent tool bridge — RBAC, audit, and the actor model

- **Status:** Accepted
- **Date:** 2026-07-27
- **Deciders:** maintainers
- **Supersedes:** none

## Context

WS-31's pillar is that the agent is **not a privileged super-user** — it is an
authorized actor that inherits the calling user's RBAC permissions and tenant
context, so the same rules that gate the dashboard gate the agent. The DoD
requires: at least one tool per in-scope module (compute, DNS, storage,
billing-usage, audit); every tool call enforces `RequirePerm` + tenant
scoping; and every privileged tool call emits an audit event before and after
with the agent recorded as the actor.

The LLMHarness executes read-only tools **inside** its turn loop (so it can
feed results back and answer), which means the enforcement cannot live only in
the HTTP handler or the service — it must ride with the executor the harness
holds.

Two sub-questions were open in the WS-31 brief:

- **Actor model** — a new `actor_type = "agent"` (with `on-behalf-of` user) vs.
  reusing `actor_type = "user"` with a `via_agent` flag.
- **Where RBAC + audit live** — in the bridge, in the service, or in a
  decorator.

Options considered:

- **Option A — a new `actor_type = "agent"`** — clean conceptual model; every
  audit row needs an `on_behalf_of_user_id` column + migration; the audit
  query API + redaction rules gain a branch. — *pro:* explicit; *con:* schema
  + wider surface change for a v1 slice.
- **Option B — `actor_type = "user"` + `metadata.via_agent = true`** — reuses
  the existing audit schema; the actor IS the user (because the agent only
  ever acts with that user's permissions); the flag lets the audit query API
  + UI distinguish agent-driven rows. — *pro:* no migration; truthful (the
  user authorized the action); *con:* "agent" is not a first-class actor.
- **Option C — enforcement in the bridge** vs. **Option D — a decorator** —
  the bridge is the natural home for tool logic; a decorator keeps RBAC +
  audit orthogonal and composable with future bridges (e.g. an MCP-server
  bridge).

## Decision

1. **Actor model (Option B):** agent-driven actions are recorded with
   `actor_type = "user"` and `metadata.via_agent = true`; the `ActorUserID`
   is the calling user. No new audit column. The existing
   `audit.ActionAgentToolExecute` + `ResourceToolCall` constants identify
   agent tool rows.
2. **Enforcement (Option D):** an `EnforcingExecutor`
   (`internal/app/lahijan/agent/enforcing_executor.go`) decorates the
   `ModuleToolBridge`. Every `Execute` resolves the actor (user id from
   `WithActorUserID` context; tenant id from `database.WithTenant`) and calls
   `rbac.Require(ctx, policy, userID, tenantID, slug)`; on success it emits a
   `pending` audit row, runs the inner tool, and `MarkOutcome`s it success or
   failure. The harness + the service both hold the enforcer, so the
   read-only loop, the legacy inline path, and the HITL confirm path are all
   gated identically.
3. The tool→permission map (`toolPermission`) lives with the enforcer; each
   advertised tool maps to the matching module read permission
   (`dns.zone.read`, `compute.instance.read`, `s3.bucket.read`,
   `billing.ledger.read`, `audit.read`).

## Consequences

- **Positive:** the agent is gated exactly like a manual UI action; audit
   trail is uniform; a future MCP-server bridge slots in behind the same
   decorator with no RBAC/audit rewrite.
- **Negative:** the actor is not first-class "agent"; an operator looking for
   "everything the agent did" must filter `metadata.via_agent = true`.
- **Neutral:** the bridge is currently read-only, so `Destructive()` is always
   false; destructive module tools (create/delete) will reuse the existing
   HITL confirm flow and the same enforcer when they land.

## Compliance

`EnforcingExecutor` wraps `ModuleToolBridge` in `program/agent_module.go`;
`WithActorUserID` is injected in `Service.StreamMessage` (+ the confirm path);
the audit action is `audit.ActionAgentToolExecute`; tool→permission pairs are
in `toolPermission`.

## References

- WS-31 brief — "MCP tool bridge + RBAC/audit mapping" + "Agent actor model"
- ADR-0002 (tenant scoping at the repository seam), ADR-0041 (harness seam)
- `internal/app/lahijan/auth/rbac/require.go` (service-layer `Require`)
