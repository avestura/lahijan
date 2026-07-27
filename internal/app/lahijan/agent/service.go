// Package agent: service.go is the entrypoint every agent API handler talks
// to. It owns conversation CRUD, the streamed agent turn, the human-in-the-
// loop tool-call state machine, the per-user BYOK provider keys (AES-GCM
// encrypted), and the per-tenant policy (allowlist / rate / spend /
// force-admin-models / tool denylist).
//
// RBAC is enforced by the api/middleware RequirePerm gate on the HTTP
// surface; tenant + user scoping is enforced by the repository seam. The
// service never trusts a caller-supplied tenant id or user id beyond what
// the context + the authenticated principal provide.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// Resource types emitted on audit rows.
const (
	ResourceConversation   = "agent_conversation"
	ResourceMessage        = "agent_message"
	ResourceToolCall       = "agent_tool_call"
	ResourceProviderConfig = "agent_provider_config"
	ResourcePolicy         = "agent_policy"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
	historyWindow   = 20 // messages fed back to the harness as context
)

// Config carries the agent-service knobs read from conf.
type Config struct {
	// Enabled toggles the subsystem. When false the handlers degrade to
	// 501 (the api package checks via IsDisabled).
	Enabled bool
	// DefaultConversationTitle is applied when the caller does not pass one.
	DefaultConversationTitle string
	// MaxMessageBytes caps a single user prompt.
	MaxMessageBytes int
}

// Service is the agent chat entrypoint. Every method takes a context
// carrying the tenant id (set by the tenant middleware) and the caller's
// user id (passed explicitly by the handler from c.Locals).
type Service struct {
	repo    repository
	audit   audit.Emitter
	crypto  *secrets.Crypto
	policy  rbac.PolicyEvaluator
	meter   Meter
	harness Harness
	tools   ToolExecutor
	config  Config
}

// Deps bundles the agent-service dependencies. crypto, harness, and tools
// are nil-appropriate: crypto nil => BYOK key save surfaces ErrCryptoRequired;
// harness nil => a paused harness (turns produce no model output); tools nil
// => the agent has no tools to call. Policy wires the per-tool RBAC evaluator
// used by the EnforcingExecutor; nil => the executor denies every tool.
// Meter wires the billing adapter that debits admin-provided model turns
// (WS-31c); nil => admin turns are unmetered + the spend cap is not enforced.
type Deps struct {
	Repos   *database.Repos
	Audit   audit.Emitter
	Crypto  *secrets.Crypto
	Policy  rbac.PolicyEvaluator
	Meter   Meter
	Harness Harness
	Tools   ToolExecutor
	Config  Config
}

// Meter is the billing seam for admin-provided model turns. BYOK turns never
// reach it. The production adapter wraps billing.Service (program package);
// tests substitute a fake.
type Meter interface {
	// BalanceCents returns the user's current balance (credits minus charges)
	// in the same cents unit as the ledger, within the tenant in ctx.
	BalanceCents(ctx context.Context, userID uuid.UUID) (int64, error)
	// ChargeAgentTokens records a usage_event (resource_type "agent_token")
	// and debits the ledger for one admin-provided turn. Idempotent on the
	// reference (a repeat call with the same reference is a no-op). The
	// adapter derives the charge amount from the conf rate.
	ChargeAgentTokens(
		ctx context.Context,
		tenantID, userID uuid.UUID,
		usage TurnUsage,
		reference string,
	) error
}

// New builds the agent service. The harness defaults to a paused no-op when
// nil so config/policy edits still work without a model backend.
func New(deps Deps) *Service {
	s := &Service{
		repo:    deps.Repos.Agent,
		audit:   deps.Audit,
		crypto:  deps.Crypto,
		policy:  deps.Policy,
		meter:   deps.Meter,
		harness: deps.Harness,
		tools:   deps.Tools,
		config:  deps.Config,
	}
	if s.audit == nil {
		s.audit = audit.NoopEmitter{}
	}
	if s.harness == nil {
		s.harness = pausedDefault()
	}
	return s
}

// IsDisabled reports whether the subsystem is turned off. The api handlers
// use this to return the localised "feature disabled" envelope.
func (s *Service) IsDisabled() bool { return s == nil || !s.config.Enabled }

// ----- Conversation CRUD --------------------------------------------------

// CreateConversation starts a new active conversation for the caller.
func (s *Service) CreateConversation(
	ctx context.Context,
	userID uuid.UUID,
	title string,
) (gen.AgentConversation, error) {
	if s.IsDisabled() {
		return gen.AgentConversation{}, ErrDisabled
	}
	if title == "" {
		title = s.config.DefaultConversationTitle
		if title == "" {
			title = "New conversation"
		}
	}
	conv, err := s.repo.CreateConversation(ctx, userID, title)
	if err != nil {
		return gen.AgentConversation{}, fmt.Errorf("agent.conversation.create: %w", err)
	}
	s.emit(ctx, audit.ActionAgentConversationCreate, ResourceConversation, conv.ID, userID, nil)
	return conv, nil
}

// ListConversations returns the caller's conversations (newest first) plus
// the total count (for paging).
func (s *Service) ListConversations(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int,
) ([]gen.AgentConversation, int64, error) {
	if s.IsDisabled() {
		return nil, 0, ErrDisabled
	}
	lim, off := clampPage(limit, offset)
	rows, err := s.repo.ListConversations(ctx, userID, int32(lim), int32(off))
	if err != nil {
		return nil, 0, fmt.Errorf("agent.conversation.list: %w", err)
	}
	// Total is over all the user's conversations in the tenant, not just
	// this page — the dashboard needs it to render pager controls.
	total, err := s.repo.CountConversations(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("agent.conversation.count: %w", err)
	}
	return rows, total, nil
}

// GetConversation verifies the caller owns the conversation and returns it.
// A NotFound result covers both "missing" and "not yours".
func (s *Service) GetConversation(
	ctx context.Context,
	userID, id uuid.UUID,
) (gen.AgentConversation, error) {
	if s.IsDisabled() {
		return gen.AgentConversation{}, ErrDisabled
	}
	return s.ownConversation(ctx, userID, id)
}

// ListMessages returns the full ordered history of a conversation the
// caller owns.
func (s *Service) ListMessages(
	ctx context.Context,
	userID, conversationID uuid.UUID,
) ([]gen.AgentMessage, error) {
	if s.IsDisabled() {
		return nil, ErrDisabled
	}
	if _, err := s.ownConversation(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("agent.message.list: %w", err)
	}
	return rows, nil
}

// ListToolCallsForMessage returns the tool calls attached to a message in a
// conversation the caller owns.
func (s *Service) ListToolCallsForMessage(
	ctx context.Context,
	userID, messageID uuid.UUID,
) ([]gen.AgentToolCall, error) {
	if s.IsDisabled() {
		return nil, ErrDisabled
	}
	rows, err := s.repo.ListToolCallsForMessage(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("agent.tool_call.list: %w", err)
	}
	// Enforce ownership: every tool call carries the (tenant_id, user_id)
	// it was created under; if the first row's user_id does not match, the
	// caller is reaching across users. (No rows is not an error.)
	for _, r := range rows {
		if r.UserID != userID {
			return nil, ErrNotFound
		}
	}
	return rows, nil
}

// DeleteConversation removes a conversation and (via FK cascade) its history.
func (s *Service) DeleteConversation(ctx context.Context, userID, id uuid.UUID) error {
	if s.IsDisabled() {
		return ErrDisabled
	}
	if _, err := s.ownConversation(ctx, userID, id); err != nil {
		return err
	}
	if err := s.repo.DeleteConversation(ctx, userID, id); err != nil {
		return fmt.Errorf("agent.conversation.delete: %w", err)
	}
	s.emit(ctx, audit.ActionAgentConversationDelete, ResourceConversation, id, userID, nil)
	return nil
}

// SetConversationStatus flips a conversation between active and archived.
func (s *Service) SetConversationStatus(
	ctx context.Context,
	userID, id uuid.UUID,
	archived bool,
) (gen.AgentConversation, error) {
	if s.IsDisabled() {
		return gen.AgentConversation{}, ErrDisabled
	}
	status := database.AgentConversationActive
	if archived {
		status = database.AgentConversationArchived
	}
	if err := s.repo.SetConversationStatus(ctx, userID, id, status); err != nil {
		return gen.AgentConversation{}, fmt.Errorf("agent.conversation.status: %w", err)
	}
	return s.ownConversation(ctx, userID, id)
}

// ownConversation loads the conversation and collapses missing + not-yours
// into NotFound.
func (s *Service) ownConversation(
	ctx context.Context,
	userID, id uuid.UUID,
) (gen.AgentConversation, error) {
	conv, err := s.repo.GetConversation(ctx, userID, id)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.AgentConversation{}, ErrNotFound
		}
		return gen.AgentConversation{}, fmt.Errorf("agent.conversation.get: %w", err)
	}
	return conv, nil
}

// ----- The streamed agent turn -------------------------------------------

// PrecheckTurn runs the deterministic pre-flight checks for a message send
// (ownership, archived, size, rate cap, force-admin-models policy) WITHOUT
// persisting the user message or starting the turn. The HTTP handler calls it
// first so hard rejections return a normal JSON error envelope instead of a
// 200 with an inline error event. StreamMessage re-validates everything, so
// skipping a check here only changes the response shape, not correctness.
func (s *Service) PrecheckTurn(
	ctx context.Context,
	userID, conversationID uuid.UUID,
	userMessage string,
) error {
	if s.IsDisabled() {
		return ErrDisabled
	}
	if s.config.MaxMessageBytes > 0 && len(userMessage) > s.config.MaxMessageBytes {
		return ErrMessageTooLarge
	}
	conv, err := s.ownConversation(ctx, userID, conversationID)
	if err != nil {
		return err
	}
	if conv.Status == database.AgentConversationArchived {
		return ErrArchived
	}
	policy, err := s.loadPolicy(ctx)
	if err != nil {
		return fmt.Errorf("agent.policy.load: %w", err)
	}
	if err := s.enforceRateLimit(ctx, userID, policy); err != nil {
		return err
	}
	if policy.ForceAdminModels {
		return ErrForceAdminModels
	}
	return nil
}

// StreamMessage appends the user's prompt, runs one agent turn, and streams
// events to emit. The caller (api handler) wires emit to the SSE response.
// StreamMessage returns when the turn is complete (done) or the context is
// cancelled; emit errors abort the turn.
func (s *Service) StreamMessage(
	ctx context.Context,
	userID, conversationID uuid.UUID,
	userMessage string,
	emit func(Event) error,
) error {
	if s.IsDisabled() {
		return ErrDisabled
	}
	if s.config.MaxMessageBytes > 0 && len(userMessage) > s.config.MaxMessageBytes {
		return ErrMessageTooLarge
	}

	conv, err := s.ownConversation(ctx, userID, conversationID)
	if err != nil {
		return err
	}
	if conv.Status == database.AgentConversationArchived {
		return ErrArchived
	}

	policy, err := s.loadPolicy(ctx)
	if err != nil {
		return fmt.Errorf("agent.policy.load: %w", err)
	}
	if err = s.enforceRateLimit(ctx, userID, policy); err != nil {
		return err
	}

	// Persist the user's prompt first so a mid-turn failure still leaves a
	// record of what the user asked for.
	if _, err = s.repo.CreateMessage(
		ctx, userID, conversationID, database.AgentMessageRoleUser, userMessage,
	); err != nil {
		return fmt.Errorf("agent.message.send: persist user message: %w", err)
	}

	provider, err := s.resolveProvider(ctx, userID, policy)
	if err != nil {
		return err
	}

	// Spend cap (WS-31c): admin-provided models debit the user's balance.
	if spendErr := s.enforceSpendCap(ctx, userID, provider, policy); spendErr != nil {
		return spendErr
	}

	// Open an audit row up front; finalize via the deferred MarkOutcome so
	// both success and failure paths are recorded.
	auditID := s.emit(ctx, audit.ActionAgentMessageSend, ResourceMessage, conversationID, userID,
		map[string]any{"via_agent": true, "chars": len(userMessage)})
	finalStatus := audit.StatusSuccess
	defer func() {
		if auditID != uuid.Nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: finalStatus})
		}
	}()

	history, err := s.historyFor(ctx, conversationID)
	if err != nil {
		finalStatus = audit.StatusFailure
		return fmt.Errorf("agent.message.send: load history: %w", err)
	}

	// Create the assistant message empty; it is finalized with the
	// concatenated tokens when the turn ends.
	assistant, err := s.repo.CreateMessage(
		ctx, userID, conversationID, database.AgentMessageRoleAssistant, "",
	)
	if err != nil {
		finalStatus = audit.StatusFailure
		return fmt.Errorf("agent.message.send: persist assistant message: %w", err)
	}
	_ = s.repo.TouchConversation(ctx, userID, conversationID)

	tools := s.toolsFor(policy)
	// Carry the calling user's id in the context so the EnforcingExecutor
	// can run per-tool rbac.Require and the billing tool can scope user
	// reads. The tenant id already travels via database.WithTenant.
	runCtx := WithActorUserID(ctx, userID)
	req := RunRequest{
		ConversationID: conversationID,
		TenantID:       conv.TenantID,
		UserID:         userID,
		History:        history,
		UserMessage:    userMessage,
		Provider:       provider,
		Tools:          tools,
	}
	events, err := s.harness.Run(runCtx, req)
	if err != nil {
		finalStatus = audit.StatusFailure
		return fmt.Errorf("agent.message.send: harness: %w", err)
	}

	var content strings.Builder
	loopErr := s.consumeEvents(runCtx, userID, assistant.ID, conversationID, provider, policy, events, &content, emit)

	// Finalize the assistant message with whatever text accumulated.
	if err := s.repo.SetMessageContent(ctx, assistant.ID, content.String()); err != nil {
		finalStatus = audit.StatusFailure
	}
	if loopErr != nil {
		// A non-nil loopErr means consumeEvents already forwarded an
		// EventError (or the client disconnected mid-stream). Mark the
		// audit row failed but do NOT propagate: the handler would otherwise
		// emit a duplicate error frame on top of the one already streamed.
		finalStatus = audit.StatusFailure
	}
	return nil
}

// consumeEvents drains the harness event channel, persists tool-call side
// effects, meters admin-provided turns, and forwards every event to emit.
// Returns an error if the harness reported one or emit failed (which aborts
// the turn).
func (s *Service) consumeEvents(
	ctx context.Context,
	userID, assistantMessageID uuid.UUID,
	conversationID uuid.UUID,
	provider ResolvedProvider,
	policy Policy,
	events <-chan Event,
	content *strings.Builder,
	emit func(Event) error,
) error {
	for ev := range events {
		switch ev.Type {
		case EventText:
			content.WriteString(ev.Text)
			if err := emit(ev); err != nil {
				return err
			}
		case EventToolCall:
			handled, err := s.handleToolCall(ctx, userID, assistantMessageID, policy, ev, emit)
			if err != nil {
				return err
			}
			_ = handled
		case EventDone:
			s.meterTurn(ctx, userID, conversationID, assistantMessageID, provider, ev.Usage)
			return emit(ev)
		case EventError:
			_ = emit(ev)
			return errors.New("agent: harness error: " + ev.Error)
		case EventToolResult:
			// The harness must not emit results; ignore defensively.
		}
	}
	// Channel closed without an explicit Done — treat as done.
	return emit(Event{Type: EventDone})
}

// meterTurn records usage + debits the ledger for an admin-provided model
// turn. BYOK turns and turns with no reported usage are unmetered. Failures
// are best-effort: the turn already succeeded, the charge is idempotent on
// its reference, and the WS-17 metering rollup reconciles the cache; a
// metering error must never roll back a successful answer.
func (s *Service) meterTurn(
	ctx context.Context,
	userID, conversationID, messageID uuid.UUID,
	provider ResolvedProvider,
	usage *TurnUsage,
) {
	if !provider.AdminProvided || s.meter == nil || usage == nil {
		return
	}
	// Stable per-message idempotency key so a retried/duplicated turn cannot
	// double-charge.
	reference := fmt.Sprintf("agent:conv:%s:msg:%s", conversationID, messageID)
	tenantID, err := database.TenantFromContext(ctx)
	if err != nil {
		return
	}
	_ = s.meter.ChargeAgentTokens(ctx, tenantID, userID, *usage, reference)
}

// enforceSpendCap refuses an admin-provided turn when the user has no credit
// left to spend. BYOK turns bypass it (they never debit the ledger). A zero or
// absent SpendCapCredits means uncapped; the balance check still runs so a
// user with a negative balance cannot run up further charges.
func (s *Service) enforceSpendCap(
	ctx context.Context,
	userID uuid.UUID,
	provider ResolvedProvider,
	policy Policy,
) error {
	if !provider.AdminProvided || s.meter == nil {
		return nil
	}
	balance, err := s.meter.BalanceCents(ctx, userID)
	if err != nil {
		return fmt.Errorf("agent.message.send: spend cap check: %w", err)
	}
	if policy.SpendCapCredits > 0 && balance <= 0 {
		return ErrSpendCap
	}
	return nil
}

// handleToolCall persists the tool-call row and either executes it inline
// (read-only tool) or parks it as pending (destructive => HITL).
func (s *Service) handleToolCall(
	ctx context.Context,
	userID, messageID uuid.UUID,
	policy Policy,
	ev Event,
	emit func(Event) error,
) (gen.AgentToolCall, error) {
	tool := ev.Tool
	if toolDenied(tool, policy.DenyTools) {
		// Policy intervened: surface an error event but do NOT abort the
		// whole turn — one denied tool should not kill the assistant's
		// in-flight text reply. The agent learns (via the next turn's
		// history) that the tool was unavailable.
		_ = emit(Event{Type: EventError, Error: "tool denied by policy"})
		return gen.AgentToolCall{}, nil
	}
	// If the harness already executed the tool (the LLMHarness runs the
	// read-only loop itself), it attaches the Result. Persist the row as
	// executed with that result and forward it to the client; do NOT execute
	// again. Only read-only tools ever arrive here with a Result.
	if len(ev.Result) > 0 {
		row, err := s.repo.CreateToolCall(
			ctx, userID, messageID, tool, ev.Args,
			false, database.AgentToolCallStatusExecuted,
		)
		if err != nil {
			return gen.AgentToolCall{}, fmt.Errorf("agent.tool_call.create: %w", err)
		}
		_ = s.repo.SetToolCallResult(ctx, userID, row.ID, ev.Result, database.AgentToolCallStatusExecuted)
		_ = emit(Event{
			Type: EventToolResult, Tool: tool,
			ToolCallID: row.ID.String(), Result: ev.Result,
		})
		return row, nil
	}
	destructive := s.tools != nil && s.tools.Destructive(tool)
	status := database.AgentToolCallStatusExecuted
	if destructive {
		status = database.AgentToolCallStatusPending
	}
	row, err := s.repo.CreateToolCall(ctx, userID, messageID, tool, ev.Args, destructive, status)
	if err != nil {
		return gen.AgentToolCall{}, fmt.Errorf("agent.tool_call.create: %w", err)
	}
	if destructive {
		// Park for HITL: surface the pending id so the client can render a
		// confirm dialog and POST /agent/tool-calls/{id}/confirm.
		return row, emit(Event{
			Type: EventToolCall, Tool: tool, Args: ev.Args,
			ToolCallID: row.ID.String(),
		})
	}
	// Read-only: execute inline + stream the result.
	result, execErr := s.execTool(ctx, tool, ev.Args)
	if execErr != nil {
		_ = s.repo.SetToolCallResult(ctx, userID, row.ID, errJSON(execErr), database.AgentToolCallStatusFailed)
		_ = emit(Event{Type: EventToolResult, Tool: tool, ToolCallID: row.ID.String(), Result: errJSON(execErr)})
		return row, nil
	}
	_ = s.repo.SetToolCallResult(ctx, userID, row.ID, result, database.AgentToolCallStatusExecuted)
	_ = emit(Event{Type: EventToolResult, Tool: tool, ToolCallID: row.ID.String(), Result: result})
	return row, nil
}

// execTool runs the tool only when an executor is wired; otherwise it returns
// a canned "no executor" error so the flow is observable.
func (s *Service) execTool(ctx context.Context, tool string, args json.RawMessage) (json.RawMessage, error) {
	if s.tools == nil {
		return nil, ErrToolNotFound
	}
	return s.tools.Execute(ctx, tool, args)
}

// ConfirmToolCall applies a human-in-the-loop decision on a pending tool call.
// When approved the tool executes and its result is recorded (and surfaced as
// a new 'tool' message); when declined the call is marked rejected.
func (s *Service) ConfirmToolCall(
	ctx context.Context,
	userID, toolCallID uuid.UUID,
	approved bool,
) (gen.AgentToolCall, error) {
	if s.IsDisabled() {
		return gen.AgentToolCall{}, ErrDisabled
	}
	row, err := s.repo.GetToolCall(ctx, userID, toolCallID)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.AgentToolCall{}, ErrNotFound
		}
		return gen.AgentToolCall{}, fmt.Errorf("agent.tool_call.get: %w", err)
	}
	if row.Status != database.AgentToolCallStatusPending {
		return gen.AgentToolCall{}, ErrNotPending
	}

	meta := map[string]any{"via_agent": true, "tool": row.ToolName, "approved": approved}
	if !approved {
		_ = s.repo.SetToolCallStatus(ctx, userID, toolCallID, database.AgentToolCallStatusRejected)
		s.emit(ctx, audit.ActionAgentToolConfirm, ResourceToolCall, toolCallID, userID, meta)
		return s.repo.GetToolCall(ctx, userID, toolCallID)
	}

	_ = s.repo.SetToolCallStatus(ctx, userID, toolCallID, database.AgentToolCallStatusApproved)
	// Carry the actor so the EnforcingExecutor can run rbac.Require + audit
	// exactly as it does on the read-only path.
	result, execErr := s.execTool(WithActorUserID(ctx, userID), row.ToolName, row.Args)
	status := database.AgentToolCallStatusExecuted
	if execErr != nil {
		status = database.AgentToolCallStatusFailed
		result = errJSON(execErr)
	}
	_ = s.repo.SetToolCallResult(ctx, userID, toolCallID, result, status)
	// Surface the result inline as a tool message so the history shows it.
	if _, err := s.repo.CreateMessage(
		ctx, userID, row.MessageID, database.AgentMessageRoleTool, string(result),
	); err != nil {
		return gen.AgentToolCall{}, fmt.Errorf("agent.tool_call.confirm: persist result message: %w", err)
	}
	s.emit(ctx, audit.ActionAgentToolExecute, ResourceToolCall, toolCallID, userID, meta)
	return s.repo.GetToolCall(ctx, userID, toolCallID)
}

// ----- Provider config (BYOK) --------------------------------------------

// ProviderConfig is the API-facing view of a BYOK config. The encrypted key
// is never returned; only a masked hint that it is set.
type ProviderConfig struct {
	ID        uuid.UUID `json:"id"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	BaseURL   string    `json:"baseUrl"`
	Enabled   bool      `json:"enabled"`
	HasKey    bool      `json:"hasKey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListProviders returns the caller's BYOK configs (key redacted).
func (s *Service) ListProviders(
	ctx context.Context,
	userID uuid.UUID,
) ([]ProviderConfig, error) {
	if s.IsDisabled() {
		return nil, ErrDisabled
	}
	rows, err := s.repo.ListProviderConfigs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("agent.provider.list: %w", err)
	}
	out := make([]ProviderConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, toProviderConfig(r))
	}
	return out, nil
}

// SaveProvider creates a new BYOK config. The plaintext key is sealed with
// the process AES-GCM envelope before it touches the DB; only the ciphertext
// is persisted.
func (s *Service) SaveProvider(
	ctx context.Context,
	userID uuid.UUID,
	provider, model, baseURL, apiKey string,
) (ProviderConfig, error) {
	if s.IsDisabled() {
		return ProviderConfig{}, ErrDisabled
	}
	if s.crypto == nil {
		return ProviderConfig{}, ErrCryptoRequired
	}
	// Idempotency: one config per (tenant, user, provider). The unique
	// index enforces it; surface a friendly error on conflict.
	existing, err := s.repo.ListProviderConfigs(ctx, userID)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("agent.provider.list: %w", err)
	}
	for _, e := range existing {
		if e.Provider == provider {
			return ProviderConfig{}, ErrProviderExists
		}
	}
	sealed, err := s.crypto.Seal(apiKey)
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("agent.provider.save: seal key: %w", err)
	}
	row, err := s.repo.CreateProviderConfig(ctx, userID, provider, model, baseURL, []byte(sealed))
	if err != nil {
		return ProviderConfig{}, fmt.Errorf("agent.provider.save: %w", err)
	}
	s.emit(ctx, audit.ActionAgentProviderSave, ResourceProviderConfig, row.ID, userID,
		map[string]any{"via_agent": true, "provider": provider, "model": model})
	return toProviderConfig(row), nil
}

// DeleteProvider removes a BYOK config.
func (s *Service) DeleteProvider(ctx context.Context, userID, id uuid.UUID) error {
	if s.IsDisabled() {
		return ErrDisabled
	}
	if _, err := s.repo.GetProviderConfig(ctx, userID, id); err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("agent.provider.get: %w", err)
	}
	if err := s.repo.DeleteProviderConfig(ctx, userID, id); err != nil {
		return fmt.Errorf("agent.provider.delete: %w", err)
	}
	s.emit(ctx, audit.ActionAgentProviderDelete, ResourceProviderConfig, id, userID, nil)
	return nil
}

// resolveProvider picks the effective provider/model/key for a turn. Policy
// controls: force_admin_models disables BYOK; allow_models filters. Admin
// shared providers are a follow-on, so force_admin_models currently means the
// agent cannot run.
func (s *Service) resolveProvider(
	ctx context.Context,
	userID uuid.UUID,
	policy Policy,
) (ResolvedProvider, error) {
	if policy.ForceAdminModels {
		// No admin providers wired yet (WS-31 follow-on); BYOK is disabled,
		// so the agent has nothing to run on.
		return ResolvedProvider{}, ErrForceAdminModels
	}
	rows, err := s.repo.ListProviderConfigs(ctx, userID)
	if err != nil {
		return ResolvedProvider{}, fmt.Errorf("agent.provider.resolve: %w", err)
	}
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		if !modelAllowed(r.Model, policy.AllowModels) {
			continue
		}
		key, openErr := s.openKey(r.ApiKeyEncrypted)
		if openErr != nil {
			return ResolvedProvider{}, fmt.Errorf("agent.provider.resolve: decrypt key: %w", openErr)
		}
		return ResolvedProvider{
			Provider: r.Provider, Model: r.Model, BaseURL: r.BaseUrl,
			APIKey: key, AdminProvided: false,
		}, nil
	}
	// No usable config: the stub harness tolerates a zero-value provider; a
	// real harness would refuse. Falling through keeps the feature usable
	// for exploration without a configured key.
	return ResolvedProvider{}, nil
}

// openKey decrypts a stored ciphertext blob via the AES-GCM envelope. Returns
// "" when crypto is not configured (the stub harness ignores the key).
func (s *Service) openKey(cipher []byte) (string, error) {
	if s.crypto == nil {
		return "", nil
	}
	return s.crypto.Open(string(cipher))
}

// ----- Policy -------------------------------------------------------------

// Policy is the domain view of the per-tenant agent policy. Zero-value means
// "permissive": no allowlist, BYOK allowed, no rate cap, no spend cap, no
// denylist.
type Policy struct {
	AllowModels          []string
	ForceAdminModels     bool
	MaxMessagesPerWindow int32
	WindowSeconds        int32
	SpendCapCredits      int64
	DenyTools            []string
}

// GetPolicy returns the tenant's policy, or the permissive default when none
// has been set.
func (s *Service) GetPolicy(ctx context.Context) (Policy, error) {
	row, err := s.repo.GetPolicy(ctx)
	if err != nil {
		if database.IsNoRows(err) {
			// Permissive default. Non-nil slices so the DTO marshals as
			// JSON [] (not null) — the dashboard's .join() would crash on
			// null otherwise.
			return Policy{AllowModels: []string{}, DenyTools: []string{}, WindowSeconds: 60}, nil
		}
		return Policy{}, fmt.Errorf("agent.policy.get: %w", err)
	}
	return policyFromRow(row), nil
}

// UpsertPolicy creates or replaces the tenant's policy.
func (s *Service) UpsertPolicy(ctx context.Context, p Policy) (Policy, error) {
	if s.IsDisabled() {
		return Policy{}, ErrDisabled
	}
	if p.WindowSeconds <= 0 {
		p.WindowSeconds = 60
	}
	row, err := s.repo.UpsertPolicy(ctx, database.UpsertAgentPolicyParams{
		AllowModels:          p.AllowModels,
		ForceAdminModels:     p.ForceAdminModels,
		MaxMessagesPerWindow: p.MaxMessagesPerWindow,
		WindowSeconds:        p.WindowSeconds,
		SpendCapCredits:      p.SpendCapCredits,
		DenyTools:            p.DenyTools,
	})
	if err != nil {
		return Policy{}, fmt.Errorf("agent.policy.upsert: %w", err)
	}
	s.emit(ctx, audit.ActionAgentPolicyUpdate, ResourcePolicy, row.ID, uuid.Nil,
		map[string]any{"force_admin_models": p.ForceAdminModels, "deny_tools": p.DenyTools})
	return policyFromRow(row), nil
}

// loadPolicy is the internal accessor used by StreamMessage.
func (s *Service) loadPolicy(ctx context.Context) (Policy, error) {
	return s.GetPolicy(ctx)
}

// enforceRateLimit rejects when the caller has already sent MaxMessagesPerWindow
// user messages within the sliding window. Zero cap => unlimited.
func (s *Service) enforceRateLimit(ctx context.Context, userID uuid.UUID, p Policy) error {
	if p.MaxMessagesPerWindow <= 0 {
		return nil
	}
	window := time.Duration(p.WindowSeconds) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	since := time.Now().Add(-window)
	count, err := s.repo.CountMessagesSince(ctx, userID, since)
	if err != nil {
		return fmt.Errorf("agent.policy.rate_limit: %w", err)
	}
	if count >= int64(p.MaxMessagesPerWindow) {
		return ErrRateLimited
	}
	return nil
}

// toolsFor builds the per-turn tool descriptor list: every tool the executor
// exposes, minus the policy denylist.
func (s *Service) toolsFor(p Policy) []ToolDescriptor {
	if s.tools == nil {
		return nil
	}
	all := s.tools.All()
	out := make([]ToolDescriptor, 0, len(all))
	for _, t := range all {
		if toolDenied(t.Name, p.DenyTools) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// historyFor returns the last historyWindow messages as harness context.
func (s *Service) historyFor(ctx context.Context, conversationID uuid.UUID) ([]HistoryMessage, error) {
	rows, err := s.repo.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if len(rows) > historyWindow {
		rows = rows[len(rows)-historyWindow:]
	}
	out := make([]HistoryMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, HistoryMessage{Role: r.Role, Content: r.Content})
	}
	return out, nil
}

// ----- helpers -----------------------------------------------------------

// emit writes an audit row. Failures are logged via the emitter (which never
// blocks) and do not break the flow; the returned id is used for MarkOutcome
// by the few callers that need an outcome trail.
func (s *Service) emit(
	ctx context.Context,
	action, resourceType string,
	resourceID, userID uuid.UUID,
	meta map[string]any,
) uuid.UUID {
	ev := audit.Event{
		Action: action, ResourceType: resourceType,
		Status: audit.StatusSuccess, ActorType: audit.ActorUser,
		Metadata: meta,
	}
	if userID != uuid.Nil {
		ev.ActorUserID = &userID
	}
	rid := resourceID
	if rid != uuid.Nil {
		ev.ResourceID = &rid
	}
	id, err := s.audit.Emit(ctx, ev)
	if err != nil {
		// Audit must never roll back the action; the emitter already logged.
		return uuid.Nil
	}
	return id
}

func clampPage(limit, offset int) (int, int) {
	lim := defaultPageSize
	if limit > 0 {
		lim = limit
	}
	if lim > maxPageSize {
		lim = maxPageSize
	}
	off := 0
	if offset > 0 {
		off = offset
	}
	return lim, off
}

func toProviderConfig(r gen.AgentProviderConfig) ProviderConfig {
	return ProviderConfig{
		ID:        r.ID,
		Provider:  r.Provider,
		Model:     r.Model,
		BaseURL:   r.BaseUrl,
		Enabled:   r.Enabled,
		HasKey:    len(r.ApiKeyEncrypted) > 0,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func policyFromRow(r gen.AgentPolicy) Policy {
	return Policy{
		AllowModels:          r.AllowModels,
		ForceAdminModels:     r.ForceAdminModels,
		MaxMessagesPerWindow: r.MaxMessagesPerWindow,
		WindowSeconds:        r.WindowSeconds,
		SpendCapCredits:      r.SpendCapCredits,
		DenyTools:            r.DenyTools,
	}
}

// modelAllowed reports whether model passes the allowlist. Empty allowlist =>
// everything allowed. A pattern ending in '*' is a prefix match; otherwise
// exact (case-insensitive).
func modelAllowed(model string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	m := strings.ToLower(strings.TrimSpace(model))
	for _, p := range allow {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(m, strings.TrimSuffix(p, "*")) {
				return true
			}
			continue
		}
		if m == p {
			return true
		}
	}
	return false
}

// toolDenied reports whether tool is in the denylist (exact, case-insensitive).
func toolDenied(tool string, deny []string) bool {
	t := strings.ToLower(strings.TrimSpace(tool))
	for _, d := range deny {
		if strings.EqualFold(strings.TrimSpace(d), t) {
			return true
		}
	}
	return false
}

// errJSON renders an error as a {error: "..."} JSON blob for the tool-result
// column, so failed tool calls have a structured result.
func errJSON(err error) json.RawMessage {
	if err == nil {
		return json.RawMessage(`{}`)
	}
	b, mErr := json.Marshal(map[string]string{"error": err.Error()})
	if mErr != nil {
		return json.RawMessage(`{"error":"marshal failed"}`)
	}
	return b
}
