// Package agent: repository.go is the narrow persistence seam the service
// depends on. The concrete *database.AgentRepository satisfies it; tests
// pass a fake. Defining the seam here (rather than depending on
// *database.Repos directly) keeps the streaming / HITL / policy logic unit-
// testable without a Postgres instance, mirroring the per-module narrow-
// interface pattern used by compute / dns / storage.
package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// repository is the subset of *database.AgentRepository the service calls.
// Every method is (tenant_id, user_id)-scoped at the concrete layer.
//
// The seam deliberately stays a single interface: all five agent_chat tables
// form one cohesive aggregate with identical (tenant_id, user_id) scoping, so
// splitting into per-table sub-interfaces would give the service five pointers
// to the same object for no isolation gain. The interfacebloat linter (max 7)
// is suppressed here for that reason.
//
//nolint:interfacebloat // one cohesive repository seam over the agent_chat tables
type repository interface {
	CreateConversation(ctx context.Context, userID uuid.UUID, title string) (gen.AgentConversation, error)
	GetConversation(ctx context.Context, userID, id uuid.UUID) (gen.AgentConversation, error)
	ListConversations(ctx context.Context, userID uuid.UUID, limit, offset int32) ([]gen.AgentConversation, error)
	CountConversations(ctx context.Context, userID uuid.UUID) (int64, error)
	TouchConversation(ctx context.Context, userID, id uuid.UUID) error
	SetConversationStatus(ctx context.Context, userID, id uuid.UUID, status string) error
	DeleteConversation(ctx context.Context, userID, id uuid.UUID) error

	CreateMessage(ctx context.Context, userID, conversationID uuid.UUID, role, content string) (gen.AgentMessage, error)
	ListMessages(ctx context.Context, conversationID uuid.UUID) ([]gen.AgentMessage, error)
	SetMessageContent(ctx context.Context, id uuid.UUID, content string) error
	CountMessagesSince(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error)

	CreateToolCall(
		ctx context.Context, userID, messageID uuid.UUID, toolName string, args []byte,
		requiresConfirmation bool, status string,
	) (gen.AgentToolCall, error)
	GetToolCall(ctx context.Context, userID, id uuid.UUID) (gen.AgentToolCall, error)
	ListToolCallsForMessage(ctx context.Context, messageID uuid.UUID) ([]gen.AgentToolCall, error)
	SetToolCallStatus(ctx context.Context, userID, id uuid.UUID, status string) error
	SetToolCallResult(ctx context.Context, userID, id uuid.UUID, result []byte, status string) error

	CreateProviderConfig(
		ctx context.Context, userID uuid.UUID, provider, model, baseURL string, apiKeyEncrypted []byte,
	) (gen.AgentProviderConfig, error)
	GetProviderConfig(ctx context.Context, userID, id uuid.UUID) (gen.AgentProviderConfig, error)
	ListProviderConfigs(ctx context.Context, userID uuid.UUID) ([]gen.AgentProviderConfig, error)
	DeleteProviderConfig(ctx context.Context, userID, id uuid.UUID) error

	GetPolicy(ctx context.Context) (gen.AgentPolicy, error)
	UpsertPolicy(ctx context.Context, arg database.UpsertAgentPolicyParams) (gen.AgentPolicy, error)
}

// compile-time assertion that the concrete repository satisfies the seam.
var _ repository = (*database.AgentRepository)(nil)
