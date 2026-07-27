# ADR-0041: Agent model integration — OpenAI-compatible client for v1

- **Status:** Accepted
- **Date:** 2026-07-27
- **Deciders:** maintainers
- **Supersedes:** none

## Context

WS-31 specifies that the agent's model loop (streaming, tool orchestration,
provider/key management) is delegated to a shared **OpenCode** daemon driven
via `opencode-sdk-go`. That gives us a batteries-included agent runtime but
adds a hard dependency on a second long-running process and on the
`anomalyco/opencode-sdk-go` fork (whose module path + maturity were still open
questions in the WS-31 brief).

The first usable slice needed the agent to actually answer questions grounded
in live tenant data *now*, with zero extra processes to run for a local dev or
a single-container deploy.

Options considered:

- **Option A — ship the OpenCode daemon now** — matches the brief; one more
  process in the compose stack; hard dep on the SDK fork + its event/SSE/MCP
  surface; blocks the slice on that integration being stable. — *pro:* no
  custom agent loop to maintain; *con:* dep + lifecycle risk, later unblock.
- **Option B — a thin OpenAI-compatible `/chat/completions` client inside the
  backend** — every BYOK provider the user picks (OpenAI, OpenRouter, Groq,
  Together, DeepSeek, xAI, Ollama, …) already speaks this protocol; one HTTP
  client covers them; no extra process; the WS-31 `agent.Harness` seam is
  unchanged so the daemon can still replace it later. — *pro:* works today,
  no new process; *con:* we own the streaming/tool-loop code; no upstream
  agent features for free.
- **Option C — wait for the daemon** — defer the feature. Rejected: the
  feature is the point of the slice.

## Decision

For v1 the agent is driven by `LLMHarness`
(`internal/app/lahijan/agent/llm_harness.go`): an in-process client of any
OpenAI-compatible `/chat/completions` endpoint, using the user's BYOK
provider/model/key (resolved + AES-GCM-decrypted by the service). The harness
advertises tools, reassembles streamed `tool_calls`, and runs a bounded (≤6
round) read-only tool loop so the model answers from live data. The OpenCode
daemon (`opencode-sdk-go`) remains the architectural target and can replace
this file behind the **same `agent.Harness` interface** when its lifecycle +
SDK are ready.

## Consequences

- **Positive:** the agent works end-to-end today with no extra process; one
  client covers every OpenAI-compatible BYOK provider; the swap path to the
  daemon is a single seam.
- **Negative:** we own the SSE/tool-calling loop; providers whose native API
  is not OpenAI-compatible (Anthropic, Bedrock, Vertex AI, Azure OpenAI with
  deployment URLs) are unsupported until a provider adapter or the daemon
  lands.
- **Neutral:** `stream_options.include_usage` is requested so admin turns can
  be metered (ADR-0043); providers that ignore it simply produce a nil usage
  and the turn is unmetered.

## Compliance

The harness satisfies `agent.Harness` (`Run(ctx, RunRequest) (<-chan Event,
error)`). `program/agent_module.go` constructs `agent.NewLLMHarness()`; the
provider base-URL map lives in `defaultProviderBase` in `llm_harness.go`.
Replacing it with the daemon only needs a new `Harness` implementation.

## References

- WS-31 brief (`docs/workstreams/WS-31-agent-chat.md`)
- `internal/app/lahijan/agent/llm_harness.go`, `harness.go`
- ADR-0042 (tool bridge), ADR-0043 (provider/limits/metering)
