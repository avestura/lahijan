# ADR-0043: Agent provider config, admin limits, and token metering

- **Status:** Accepted
- **Date:** 2026-07-27
- **Deciders:** maintainers
- **Supersedes:** none

## Context

WS-31 needs (a) BYOK provider keys stored encrypted at rest, (b) the five
admin controls enforced (model allowlist, per-window rate limit, spend cap,
force-admin-models, tool denylist), and (c) token usage metered into the WS-17
ledger **only when the platform pays for the model** (admin-provided models);
BYOK traffic is unmetered and uncharged.

Forces:

- Billing must reuse the canonical WS-17 charge path (`billing.Service.PostCharge`)
  so the balance cache, events, and idempotency behave like every other charge.
- The price-per-token is a product decision the WS-31 brief left open; the
  WS-17 price catalog does not yet key on `agent_token`.
- Admin-shared (platform-level) providers require a new table + migration +
  sqlc regen; the BYOK-only table (`agent_provider_configs`, migration 0049)
  cannot represent them.

Options considered for **metering**:

- **Option A — roll up via the WS-17 metering job** — write `usage_events`,
  let the periodic collector post the charge. — *pro:* consistent with other
  resources; *con:* async, so the spend cap can't be enforced pre-send.
- **Option B — synchronous charge on turn end** — record the `usage_event`
  AND call `PostCharge` immediately. — *pro:* spend cap is enforceable
  pre-send + the ledger is current; *con:* a synchronous DB write per turn.

## Decision

1. **Provider keys (BYOK):** stored AES-256-GCM encrypted in
   `agent_provider_configs.api_key_encrypted` (migration 0049); the raw key is
   never logged and never returned by the API. Resolution picks the first
   enabled config whose model passes the tenant allowlist; the decrypted key
   is handed to the harness for the turn only.
2. **Metering (Option B):** when a turn runs on an admin-provided model, the
   harness reports token usage (`stream_options.include_usage`) on the
   terminal `EventDone`; the service calls `agent.Meter.ChargeAgentTokens`,
   which writes a `usage_events` row (`resource_type = "agent_token"`,
   `unit = "tokens"`) and debits the ledger via
   `billing.Service.PostCharge` with `source = "agent"`. BYOK turns never
   reach the meter. The reference `agent:conv:<id>:msg:<id>` is the
   idempotency key on both rows so a retried/duplicated turn cannot
   double-count.
3. **Price (placeholder):** `conf.agent.billing.centsPer1kTokens` (default 2
   = $0.02 / 1k tokens) is the interim rate, overridable by env. It is a
   documented placeholder pending a `agent_token` entry in the WS-17 price
   catalog; operators tune it until then.
4. **Spend cap:** before an admin-provided turn runs, `enforceSpendCap`
   reads the cached balance; a tenant with `SpendCapCredits > 0` and a
   balance `<= 0` is refused (`ErrSpendCap`). BYOK turns bypass the check.
5. **The other three controls** (allowlist, rate limit, force-admin-models)
   ship as today; admin-shared providers + the force-admin-models *real*
   fallback are deferred (see Consequences).

## Consequences

- **Positive:** admin turns are metered + charged through the canonical path;
   spend cap is real; BYOK stays free; idempotency prevents double-counting.
- **Negative:** the placeholder rate is not the price catalog; admin-shared
   providers + the `force_admin_models` real path are NOT in this slice —
   until a `0050_agent_admin_providers` migration lands, `force_admin_models`
   disables BYOK and the agent has nothing to run on (`ErrForceAdminModels`).
- **Neutral:** metering is best-effort on turn success (a charge failure does
   not roll back the answer); the WS-17 rollup reconciles the balance cache.

## Compliance

`agent.Meter` is the seam; `program/agent_meter.go` adapts `billing.Service`;
`Service.meterTurn` + `Service.enforceSpendCap` are the call sites; the conf
knob is `agent.billing.centsPer1kTokens`
(`internal/app/lahijan/conf/.lahijan.conf.default.yaml`).

## References

- WS-31 brief — "Provider config, secrets & admin limits" + "Token metering"
- ADR for WS-17 (billing/metering), `internal/app/lahijan/billing/ledger.go`
- ADR-0041 (harness seam), ADR-0042 (tool bridge)
