// Package database: agent_repo.go wraps the sqlc-generated agent_chat queries
// (WS-31). Every query is scoped by the tenant id pulled from the request
// context (database.WithTenant) AND, where chat history is personal, by the
// caller's user id. The user id is passed explicitly by the service (it
// resolves it from c.Locals) because — unlike tenant id — there is no
// process-wide user context seam; making it an explicit parameter keeps the
// authorization boundary visible at every call site.
//
// The conversation/message/tool-call tree is delete-cascaded at the DB layer
// (agent_messages FK -> agent_conversations, agent_tool_calls FK ->
// agent_messages), so deleting a conversation reaps its whole history without
// the service needing to walk the tree.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// Agent conversation + message + tool-call status / role constants. Mirror the
// CHECK constraints added in migration 0049 so a typo cannot strand a row.
const (
	AgentConversationActive   = "active"
	AgentConversationArchived = "archived"

	AgentMessageRoleUser      = "user"
	AgentMessageRoleAssistant = "assistant"
	AgentMessageRoleTool      = "tool"

	AgentToolCallStatusPending  = "pending"
	AgentToolCallStatusApproved = "approved"
	AgentToolCallStatusRejected = "rejected"
	AgentToolCallStatusExecuted = "executed"
	AgentToolCallStatusFailed   = "failed"
)

// AgentRepository is the persistence boundary for the agent_chat tables.
// A single repository covers all five tables because they share the same
// (tenant_id, user_id) scoping seam; splitting per-table would force the
// service to hold five pointers for no isolation gain.
type AgentRepository struct {
	q *gen.Queries
}

// NewAgentRepository wraps the given sqlc queries.
func NewAgentRepository(q *gen.Queries) *AgentRepository {
	return &AgentRepository{q: q}
}

// CreateConversation inserts a new active conversation owned by userID.
func (r *AgentRepository) CreateConversation(
	ctx context.Context,
	userID uuid.UUID,
	title string,
) (gen.AgentConversation, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentConversation{}, err
	}
	return r.q.CreateAgentConversation(ctx, gen.CreateAgentConversationParams{
		TenantID: tenantID, UserID: userID, Title: title,
	})
}

// GetConversation returns the conversation only if it belongs to userID in
// the tenant in ctx. A no-rows result means "not found OR not yours" — the
// service surfaces both as NotFound to avoid leaking existence.
func (r *AgentRepository) GetConversation(
	ctx context.Context,
	userID, id uuid.UUID,
) (gen.AgentConversation, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentConversation{}, err
	}
	return r.q.GetAgentConversationByID(ctx, gen.GetAgentConversationByIDParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// ListConversations returns the user's conversations in the tenant, newest
// first, paginated.
func (r *AgentRepository) ListConversations(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.AgentConversation, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAgentConversations(ctx, gen.ListAgentConversationsParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// CountConversations returns the total number of conversations the caller
// owns in the tenant (for paging).
func (r *AgentRepository) CountConversations(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountAgentConversations(ctx, gen.CountAgentConversationsParams{
		TenantID: tenantID, UserID: userID,
	})
}

// TouchConversation bumps updated_at so the conversation sorts to the top of
// the user's list after a new message.
func (r *AgentRepository) TouchConversation(
	ctx context.Context,
	userID, id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.TouchAgentConversation(ctx, gen.TouchAgentConversationParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// SetConversationTitle updates the human-readable title.
func (r *AgentRepository) SetConversationTitle(
	ctx context.Context,
	userID, id uuid.UUID,
	title string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetAgentConversationTitle(ctx, gen.SetAgentConversationTitleParams{
		TenantID: tenantID, UserID: userID, ID: id, Title: title,
	})
}

// SetConversationStatus flips a conversation between active and archived.
func (r *AgentRepository) SetConversationStatus(
	ctx context.Context,
	userID, id uuid.UUID,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetAgentConversationStatus(ctx, gen.SetAgentConversationStatusParams{
		TenantID: tenantID, UserID: userID, ID: id, Status: status,
	})
}

// DeleteConversation removes the conversation and (via FK cascade) its
// messages + tool calls.
func (r *AgentRepository) DeleteConversation(
	ctx context.Context,
	userID, id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteAgentConversation(ctx, gen.DeleteAgentConversationParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// CreateMessage appends a message to the conversation.
func (r *AgentRepository) CreateMessage(
	ctx context.Context,
	userID, conversationID uuid.UUID,
	role, content string,
) (gen.AgentMessage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentMessage{}, err
	}
	return r.q.CreateAgentMessage(ctx, gen.CreateAgentMessageParams{
		ConversationID: conversationID,
		TenantID:       tenantID,
		UserID:         userID,
		Role:           role,
		Content:        content,
	})
}

// ListMessages returns the full ordered history of a conversation. Tenant
// scoping is enforced; conversation ownership is verified by the caller via
// GetConversation before this runs.
func (r *AgentRepository) ListMessages(
	ctx context.Context,
	conversationID uuid.UUID,
) ([]gen.AgentMessage, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAgentMessages(ctx, gen.ListAgentMessagesParams{
		TenantID: tenantID, ConversationID: conversationID,
	})
}

// SetMessageContent finalizes a streamed assistant message.
func (r *AgentRepository) SetMessageContent(
	ctx context.Context,
	id uuid.UUID,
	content string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetAgentMessageContent(ctx, gen.SetAgentMessageContentParams{
		TenantID: tenantID, ID: id, Content: content,
	})
}

// CountMessagesSince returns how many user-role messages the caller has sent
// since `since` — the rate-limit window counter.
func (r *AgentRepository) CountMessagesSince(
	ctx context.Context,
	userID uuid.UUID,
	since time.Time,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountAgentMessagesSince(ctx, gen.CountAgentMessagesSinceParams{
		TenantID: tenantID, UserID: userID, CreatedAt: since,
	})
}

// CreateToolCall records a tool invocation against a message.
func (r *AgentRepository) CreateToolCall(
	ctx context.Context,
	userID, messageID uuid.UUID,
	toolName string,
	args []byte,
	requiresConfirmation bool,
	status string,
) (gen.AgentToolCall, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentToolCall{}, err
	}
	return r.q.CreateAgentToolCall(ctx, gen.CreateAgentToolCallParams{
		MessageID:            messageID,
		TenantID:             tenantID,
		UserID:               userID,
		ToolName:             toolName,
		Args:                 args,
		RequiresConfirmation: requiresConfirmation,
		Status:               status,
	})
}

// GetToolCall returns the tool call only if it belongs to userID.
func (r *AgentRepository) GetToolCall(
	ctx context.Context,
	userID, id uuid.UUID,
) (gen.AgentToolCall, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentToolCall{}, err
	}
	return r.q.GetAgentToolCallByID(ctx, gen.GetAgentToolCallByIDParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// ListToolCallsForMessage returns the ordered tool calls attached to a message.
func (r *AgentRepository) ListToolCallsForMessage(
	ctx context.Context,
	messageID uuid.UUID,
) ([]gen.AgentToolCall, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAgentToolCallsForMessage(ctx, gen.ListAgentToolCallsForMessageParams{
		TenantID: tenantID, MessageID: messageID,
	})
}

// SetToolCallStatus transitions a tool call (approved / rejected) without a
// result — used by the HITL confirm path.
func (r *AgentRepository) SetToolCallStatus(
	ctx context.Context,
	userID, id uuid.UUID,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetAgentToolCallStatus(ctx, gen.SetAgentToolCallStatusParams{
		TenantID: tenantID, UserID: userID, ID: id, Status: status,
	})
}

// SetToolCallResult records the executor's result and the final status.
func (r *AgentRepository) SetToolCallResult(
	ctx context.Context,
	userID, id uuid.UUID,
	result []byte,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetAgentToolCallResult(ctx, gen.SetAgentToolCallResultParams{
		TenantID: tenantID, UserID: userID, ID: id, Result: result, Status: status,
	})
}

// CreateProviderConfig inserts a new BYOK provider config. The api_key blob is
// AES-256-GCM ciphertext produced by the service; the repo never sees plaintext.
func (r *AgentRepository) CreateProviderConfig(
	ctx context.Context,
	userID uuid.UUID,
	provider, model, baseURL string,
	apiKeyEncrypted []byte,
) (gen.AgentProviderConfig, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentProviderConfig{}, err
	}
	return r.q.CreateAgentProviderConfig(ctx, gen.CreateAgentProviderConfigParams{
		TenantID:        tenantID,
		UserID:          userID,
		Provider:        provider,
		Model:           model,
		BaseUrl:         baseURL,
		ApiKeyEncrypted: apiKeyEncrypted,
	})
}

// GetProviderConfig returns the config only if it belongs to userID.
func (r *AgentRepository) GetProviderConfig(
	ctx context.Context,
	userID, id uuid.UUID,
) (gen.AgentProviderConfig, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentProviderConfig{}, err
	}
	return r.q.GetAgentProviderConfig(ctx, gen.GetAgentProviderConfigParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// ListProviderConfigs returns the user's BYOK configs in the tenant.
func (r *AgentRepository) ListProviderConfigs(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.AgentProviderConfig, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAgentProviderConfigs(ctx, gen.ListAgentProviderConfigsParams{
		TenantID: tenantID, UserID: userID,
	})
}

// DeleteProviderConfig removes a BYOK config.
func (r *AgentRepository) DeleteProviderConfig(
	ctx context.Context,
	userID, id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteAgentProviderConfig(ctx, gen.DeleteAgentProviderConfigParams{
		TenantID: tenantID, UserID: userID, ID: id,
	})
}

// GetPolicy returns the tenant's agent policy. Returns the gen "no rows"
// error when no row has been set; the service treats that as the permissive
// default.
func (r *AgentRepository) GetPolicy(ctx context.Context) (gen.AgentPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentPolicy{}, err
	}
	return r.q.GetAgentPolicy(ctx, tenantID)
}

// UpsertAgentPolicyParams carries the user-editable policy fields.
type UpsertAgentPolicyParams = gen.UpsertAgentPolicyParams

// UpsertPolicy creates or replaces the tenant's agent policy row.
func (r *AgentRepository) UpsertPolicy(
	ctx context.Context,
	arg UpsertAgentPolicyParams,
) (gen.AgentPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AgentPolicy{}, err
	}
	arg.TenantID = tenantID
	return r.q.UpsertAgentPolicy(ctx, arg)
}
