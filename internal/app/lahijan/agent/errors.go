// Package agent implements Lahijan's AI chat module (WS-31). It owns the
// conversation lifecycle, the per-message agent turn (streamed), the
// per-tool-call human-in-the-loop state machine, the per-user BYOK
// model-provider keys (AES-GCM encrypted), and the per-tenant agent policy
// (allowlist / rate / spend / force-admin-models / tool denylist).
//
// Layering:
//
//	api/agent_handlers.go -> agent.Service -> database.AgentRepository
//	                                   \------> auth/audit (emitter)
//	                                   \------> auth/secrets (AES-GCM crypto)
//	                                   \------> Harness (OpenCode harness; stub here)
//	                                   \------> ToolExecutor (MCP tool bridge; stub here)
//
// The agent is an authorized actor that inherits the calling user's RBAC
// context: the api/middleware gate enforces agent.* permissions on the HTTP
// surface, and the repository seam enforces (tenant_id, user_id) scoping on
// every query. The real model-driven agent loop + the MCP tool bridge that
// maps Lahijan module actions to agent tools land in follow-on sub-streams
// (WS-31a/b); this package ships the full mechanism behind small Harness /
// ToolExecutor seams so the flow is end-to-end testable today.
package agent

import "errors"

// Sentinel errors. The api handler translates these to the standard error
// envelope via the agent mapAgentError helper.
var (
	// ErrDisabled is returned when the agent subsystem is not wired
	// (conf.agent.enabled = false or no harness configured).
	ErrDisabled = errors.New("agent: subsystem disabled")

	// ErrNotFound is returned when a conversation / message / tool call /
	// provider config does not exist OR exists but belongs to another
	// user. The two cases are deliberately collapsed to avoid leaking
	// existence across users.
	ErrNotFound = errors.New("agent: not found")

	// ErrCryptoRequired is returned when a BYOK provider key is being
	// saved but the process has no AES-GCM encryption envelope (dev
	// without auth.secrets.encryptionKey set).
	ErrCryptoRequired = errors.New("agent: encryption envelope required to store provider keys")

	// ErrProviderExists is returned when the user already has a config for
	// the same provider in this tenant.
	ErrProviderExists = errors.New("agent: provider config already exists")

	// ErrRateLimited is returned when the tenant policy's sliding-window
	// message cap has been reached.
	ErrRateLimited = errors.New("agent: rate limit reached")

	// ErrSpendCap is returned when the tenant policy's spend cap has been
	// reached. Enforcement lands with the token-metering follow-on; the
	// field is stored + surfaced today.
	ErrSpendCap = errors.New("agent: spend cap reached")

	// ErrForceAdminModels is returned when the tenant policy disables BYOK
	// (force_admin_models = true). Admin-shared providers are a follow-on;
	// until they ship, this policy effectively turns the agent off for the
	// tenant.
	ErrForceAdminModels = errors.New("agent: user providers disabled by policy")

	// ErrModelNotAllowed is returned when the resolved model is not in the
	// tenant policy's allow_models list.
	ErrModelNotAllowed = errors.New("agent: model not allowed by policy")

	// ErrToolDenied is returned when the agent attempts a tool listed in
	// the tenant policy's deny_tools.
	ErrToolDenied = errors.New("agent: tool denied by policy")

	// ErrToolNotFound is returned by the ToolExecutor when the agent calls
	// a tool the executor does not know.
	ErrToolNotFound = errors.New("agent: tool not found")

	// ErrNotPending is returned when the caller tries to confirm a tool
	// call that is not in the pending state.
	ErrNotPending = errors.New("agent: tool call is not pending confirmation")

	// ErrArchived is returned when the caller tries to send a message to
	// an archived (read-only) conversation.
	ErrArchived = errors.New("agent: conversation is archived")

	// ErrMessageTooLarge is returned when the user's prompt exceeds the
	// configured MaxMessageBytes.
	ErrMessageTooLarge = errors.New("agent: message too large")
)
