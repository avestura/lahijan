// Package program: agent_module.go builds the WS-31 agent chat service.
//
// The service is opt-in (conf.agent.enabled). When enabled it is wired with:
//   - the shared repository aggregate + audit emitter,
//   - the AES-GCM crypto envelope reused from auth (for BYOK key encryption),
//   - the LLMHarness (OpenAI-compatible /chat/completions) with the read-only
//     ModuleToolBridge, so the agent can call dns/compute/storage list tools
//     and answer from live data.
//
// The real OpenCode harness (opencode-sdk-go) + the destructive half of the
// tool bridge (create/delete/modify with HITL) land in follow-on sub-streams
// (WS-31a/b); swapping them in only needs to satisfy the agent.Harness +
// agent.ToolExecutor seams.
package program

import (
	"log/slog"

	"github.com/avestura/lahijan/internal/app/lahijan/agent"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// buildAgentService builds the agent chat service from config. Returns nil
// (handlers degrade to 501) when the subsystem is disabled. crypto may be nil
// in dev without an encryption key; the service then surfaces
// agent.ErrCryptoRequired on BYOK key save.
func buildAgentService(
	repos *database.Repos,
	emitter audit.Emitter,
	crypto *secrets.Crypto,
	billingSvc *billing.Service,
	log *slog.Logger,
) *agent.Service {
	if !conf.GetAgentEnabled() {
		log.Debug("agent subsystem is disabled; skipping agent service build")
		return nil
	}
	// The read-only module tool bridge exposes one tool per in-scope module
	// (dns/compute/storage/billing-usage/audit) over the shared repos. It is
	// wrapped by the EnforcingExecutor so every call is permission-gated via
	// rbac.Require and emits an audit row before + after (pillar 2 + 7). The
	// enforcer is wired both as the service's ToolExecutor (so the descriptor
	// list the policy filters is the real catalog) and into the harness (so
	// the LLMHarness runs the read-only tool loop and feeds results back to
	// the model for a grounded, natural-language answer).
	bridge := agent.NewModuleToolBridge(repos)
	policy := rbac.NewEvaluator(repos.Memberships)
	tools := agent.NewEnforcingExecutor(bridge, policy, emitter)
	// Token metering (WS-31c): admin-provided turns record a usage_event +
	// debit the ledger; the spend-cap pre-check reads the balance. BYOK turns
	// never reach the meter. nil billingSvc => metering disabled.
	var meter agent.Meter
	if billingSvc != nil {
		meter = newAgentMeter(billingSvc, repos.BillingUsage, conf.GetAgentBillingCentsPer1kTokens())
	}
	svc := agent.New(agent.Deps{
		Repos:  repos,
		Audit:  emitter,
		Crypto: crypto,
		Policy: policy,
		Meter:  meter,
		// The real model-backed harness. It talks to any OpenAI-compatible
		// /chat/completions endpoint using the user's BYOK provider config
		// (OpenAI / OpenRouter / Groq / Ollama / ...). The OpenCode daemon
		// (opencode-sdk-go) is the architectural target (WS-31a) and can
		// replace this behind the same agent.Harness seam.
		Harness: agent.NewLLMHarness().WithToolExecutor(tools),
		Tools:   tools,
		Config: agent.Config{
			Enabled:                  true,
			DefaultConversationTitle: conf.GetAgentDefaultConversationTitle(),
			MaxMessageBytes:          conf.GetAgentMaxMessageBytes(),
		},
	})
	log.Info("agent chat subsystem enabled",
		"harness", "llm", "tools", "enforcing-bridge", "meter", meter != nil)
	return svc
}
