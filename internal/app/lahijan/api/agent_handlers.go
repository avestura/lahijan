// Package api: agent_handlers.go implements the OpenAPI-derived agent chat
// endpoints (WS-31). Each handler is thin: resolve tenant + user from the
// request scope, parse the body, call the agent service, render the response.
// Every privileged route is gated by RequirePerm via the audit gate in
// router.go (extended to cover /api/v1/agent/*).
//
// The send-message endpoint streams the agent turn back as Server-Sent
// Events (Content-Type: text/event-stream). The service produces a stream of
// agent.Event values; this handler adapts each one to an SSE `data:` frame.
// Streaming happens inside fasthttp's SetBodyStreamWriter so the connection
// stays open for the whole turn.
package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/agent"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	dbgen "github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// agentDisabled reports whether the agent subsystem is not wired. Returns the
// localised disabled envelope when so.
func (s *Server) agentDisabled(c *fiber.Ctx) bool {
	if s.agentSvc != nil && !s.agentSvc.IsDisabled() {
		return false
	}
	_ = SendError(c, fiber.StatusNotImplemented, CodeNotImplemented,
		i18n.T(c.UserContext(), "agent.err_disabled", nil), nil)
	return true
}

// agentUser resolves the authenticated user id; returns false (after sending a
// 401) when no session is present.
func (s *Server) agentUser(c *fiber.Ctx) (uuid.UUID, bool) {
	uid, ok := currentUserID(c)
	if !ok {
		_ = SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		return uuid.Nil, false
	}
	return uid, true
}

// ListAgentConversations handles GET /api/v1/agent/conversations.
func (s *Server) ListAgentConversations(c *fiber.Ctx, params apigen.ListAgentConversationsParams) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	limit, offset := agentPageParams(params.Limit, params.Offset)
	rows, total, err := s.agentSvc.ListConversations(c.UserContext(), uid, limit, offset)
	if err != nil {
		return mapAgentError(c, err)
	}
	items := make([]apigen.AgentConversation, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAgentConversationDTO(r))
	}
	return c.JSON(apigen.AgentConversationList{Items: items, Total: int(total)})
}

// CreateAgentConversation handles POST /api/v1/agent/conversations.
func (s *Server) CreateAgentConversation(c *fiber.Ctx) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	var req apigen.CreateAgentConversationRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_bad_request", nil), nil)
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	conv, err := s.agentSvc.CreateConversation(c.UserContext(), uid, title)
	if err != nil {
		return mapAgentError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAgentConversationDTO(conv))
}

// GetAgentConversation handles GET /api/v1/agent/conversations/{conversationID}.
func (s *Server) GetAgentConversation(c *fiber.Ctx, conversationID apigen.ConversationId) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	conv, err := s.agentSvc.GetConversation(c.UserContext(), uid, conversationID)
	if err != nil {
		return mapAgentError(c, err)
	}
	msgs, err := s.agentSvc.ListMessages(c.UserContext(), uid, conversationID)
	if err != nil {
		return mapAgentError(c, err)
	}
	// Gather tool calls across every message in one pass; the message each
	// belongs to is carried by ToolCall.MessageId.
	var toolCalls []apigen.AgentToolCall
	for _, m := range msgs {
		rows, err := s.agentSvc.ListToolCallsForMessage(c.UserContext(), uid, m.ID)
		if err != nil {
			return mapAgentError(c, err)
		}
		for _, tc := range rows {
			toolCalls = append(toolCalls, toAgentToolCallDTO(tc))
		}
	}
	if toolCalls == nil {
		toolCalls = []apigen.AgentToolCall{}
	}
	msgDTOs := make([]apigen.AgentMessage, 0, len(msgs))
	for _, m := range msgs {
		msgDTOs = append(msgDTOs, toAgentMessageDTO(m))
	}
	return c.JSON(apigen.AgentConversationDetail{
		Conversation: toAgentConversationDTO(conv),
		Messages:     msgDTOs,
		ToolCalls:    toolCalls,
	})
}

// DeleteAgentConversation handles DELETE /api/v1/agent/conversations/{conversationID}.
func (s *Server) DeleteAgentConversation(c *fiber.Ctx, conversationID apigen.ConversationId) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	if err := s.agentSvc.DeleteConversation(c.UserContext(), uid, conversationID); err != nil {
		return mapAgentError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SendAgentMessage handles POST /api/v1/agent/conversations/{conversationID}/messages.
//
// The response is an SSE stream. We resolve the caller + body in the handler
// goroutine, then hand a captured context + emit callback to fasthttp's
// stream writer. The service runs the turn synchronously inside the writer;
// each agent.Event becomes a `data: <json>\n\n` frame.
func (s *Server) SendAgentMessage(c *fiber.Ctx, conversationID apigen.ConversationId) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	var req apigen.SendAgentMessageRequest
	if err := c.BodyParser(&req); err != nil || req.Message == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_bad_request", nil), nil)
	}

	// Capture the tenant-scoped user context NOW: the stream writer runs in
	// a separate goroutine and must not touch the pooled fasthttp Ctx.
	streamCtx := c.UserContext()
	uidCapture := uid
	convID := conversationID
	msg := req.Message
	svc := s.agentSvc

	// Pre-flight the turn so hard rejections (rate cap, force-admin-models,
	// archived, not found) return a normal JSON error envelope instead of a
	// 200 with an inline error event. The service re-validates inside the
	// turn; this just chooses the response shape for deterministic failures.
	if err := svc.PrecheckTurn(streamCtx, uidCapture, convID, msg); err != nil {
		return mapAgentError(c, err)
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // defeat proxy buffering (nginx)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		emit := func(e agent.Event) error {
			payload, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return err
			}
			return w.Flush()
		}
		if err := svc.StreamMessage(streamCtx, uidCapture, convID, msg, emit); err != nil {
			errEvent, _ := json.Marshal(agent.Event{Type: agent.EventError, Error: err.Error()})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", errEvent)
			_ = w.Flush()
		}
	})
	return nil
}

// ConfirmAgentToolCall handles POST /api/v1/agent/tool-calls/{toolCallID}/confirm.
func (s *Server) ConfirmAgentToolCall(c *fiber.Ctx, toolCallID apigen.ToolCallId) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	var req apigen.ConfirmAgentToolCallRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_bad_request", nil), nil)
	}
	row, err := s.agentSvc.ConfirmToolCall(c.UserContext(), uid, toolCallID, req.Approved)
	if err != nil {
		return mapAgentError(c, err)
	}
	return c.JSON(toAgentToolCallDTO(row))
}

// ListAgentProviders handles GET /api/v1/agent/providers.
func (s *Server) ListAgentProviders(c *fiber.Ctx) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	rows, err := s.agentSvc.ListProviders(c.UserContext(), uid)
	if err != nil {
		return mapAgentError(c, err)
	}
	out := make([]apigen.AgentProviderConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAgentProviderConfigDTO(r))
	}
	return c.JSON(out)
}

// CreateAgentProvider handles POST /api/v1/agent/providers.
func (s *Server) CreateAgentProvider(c *fiber.Ctx) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	var req apigen.AgentProviderConfigRequest
	if err := c.BodyParser(&req); err != nil || req.Provider == "" || req.ApiKey == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_bad_request", nil), nil)
	}
	model := ""
	if req.Model != nil {
		model = *req.Model
	}
	baseURL := ""
	if req.BaseUrl != nil {
		baseURL = *req.BaseUrl
	}
	row, err := s.agentSvc.SaveProvider(c.UserContext(), uid, req.Provider, model, baseURL, req.ApiKey)
	if err != nil {
		return mapAgentError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAgentProviderConfigDTO(row))
}

// DeleteAgentProvider handles DELETE /api/v1/agent/providers/{providerID}.
func (s *Server) DeleteAgentProvider(c *fiber.Ctx, providerID apigen.ProviderId) error {
	if s.agentDisabled(c) {
		return nil
	}
	uid, ok := s.agentUser(c)
	if !ok {
		return nil
	}
	if err := s.agentSvc.DeleteProvider(c.UserContext(), uid, providerID); err != nil {
		return mapAgentError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetAgentPolicy handles GET /api/v1/agent/policy.
func (s *Server) GetAgentPolicy(c *fiber.Ctx) error {
	if s.agentDisabled(c) {
		return nil
	}
	p, err := s.agentSvc.GetPolicy(c.UserContext())
	if err != nil {
		return mapAgentError(c, err)
	}
	return c.JSON(toAgentPolicyDTO(p))
}

// UpdateAgentPolicy handles PUT /api/v1/agent/policy.
func (s *Server) UpdateAgentPolicy(c *fiber.Ctx) error {
	if s.agentDisabled(c) {
		return nil
	}
	var req apigen.AgentPolicy
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_bad_request", nil), nil)
	}
	p, err := s.agentSvc.UpsertPolicy(c.UserContext(), policyFromDTO(req))
	if err != nil {
		return mapAgentError(c, err)
	}
	return c.JSON(toAgentPolicyDTO(p))
}

// --- error mapping -------------------------------------------------------

// mapAgentError translates an agent service error into the standard error
// envelope. Pre-flight / deterministic failures use i18n messages; unexpected
// ones fall back to a 500 with the wrapped error text.
func mapAgentError(c *fiber.Ctx, err error) error {
	t := i18n.T(c.UserContext(), "agent.err_bad_request", nil)
	switch {
	case errors.Is(err, agent.ErrDisabled):
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented,
			i18n.T(c.UserContext(), "agent.err_disabled", nil), nil)
	case errors.Is(err, agent.ErrNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "agent.err_not_found", nil))
	case errors.Is(err, agent.ErrArchived):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "agent.err_archived", nil), nil)
	case errors.Is(err, agent.ErrProviderExists):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "agent.err_provider_exists", nil), nil)
	case errors.Is(err, agent.ErrNotPending):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "agent.err_not_pending", nil), nil)
	case errors.Is(err, agent.ErrRateLimited), errors.Is(err, agent.ErrSpendCap):
		return SendError(c, fiber.StatusTooManyRequests, "rate_limited",
			i18n.T(c.UserContext(), "agent.err_rate_limited", nil), nil)
	case errors.Is(err, agent.ErrForceAdminModels):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(c.UserContext(), "agent.err_force_admin_models", nil), nil)
	case errors.Is(err, agent.ErrModelNotAllowed):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(c.UserContext(), "agent.err_model_not_allowed", nil), nil)
	case errors.Is(err, agent.ErrToolDenied):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(c.UserContext(), "agent.err_tool_denied", nil), nil)
	case errors.Is(err, agent.ErrCryptoRequired):
		return SendError(c, fiber.StatusInternalServerError, CodeInternal,
			i18n.T(c.UserContext(), "agent.err_crypto_required", nil), nil)
	case errors.Is(err, agent.ErrMessageTooLarge):
		return SendBadRequest(c, i18n.T(c.UserContext(), "agent.err_message_too_large", nil), nil)
	case errors.Is(err, agent.ErrToolNotFound):
		return SendError(c, fiber.StatusInternalServerError, CodeInternal,
			i18n.T(c.UserContext(), "agent.err_tool_not_found", nil), nil)
	case errors.Is(err, database.ErrNoTenantInContext):
		return SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
	}
	_ = t
	return SendError(c, fiber.StatusInternalServerError, CodeInternal, err.Error(), nil)
}

// --- pagination helper ---------------------------------------------------

// agentPageParams clamps + defaults limit/offset for the conversation list.
func agentPageParams(limit *apigen.PageLimit, offset *apigen.PageOffset) (int, int) {
	lim := 50
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > 200 {
		lim = 200
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return lim, off
}

// --- DTO mappers ---------------------------------------------------------

func toAgentConversationDTO(c dbgen.AgentConversation) apigen.AgentConversation {
	return apigen.AgentConversation{
		Id:        c.ID,
		Title:     c.Title,
		Status:    apigen.AgentConversationStatus(c.Status),
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func toAgentMessageDTO(m dbgen.AgentMessage) apigen.AgentMessage {
	return apigen.AgentMessage{
		Id:        m.ID,
		Role:      apigen.AgentMessageRole(m.Role),
		Content:   m.Content,
		CreatedAt: m.CreatedAt,
	}
}

func toAgentToolCallDTO(tc dbgen.AgentToolCall) apigen.AgentToolCall {
	return apigen.AgentToolCall{
		Id:                   tc.ID,
		MessageId:            tc.MessageID,
		Tool:                 tc.ToolName,
		Args:                 jsonObj(tc.Args),
		Result:               jsonResult(tc.Result),
		RequiresConfirmation: tc.RequiresConfirmation,
		Status:               apigen.AgentToolCallStatus(tc.Status),
		CreatedAt:            tc.CreatedAt,
		UpdatedAt:            tc.UpdatedAt,
	}
}

func toAgentProviderConfigDTO(p agent.ProviderConfig) apigen.AgentProviderConfig {
	return apigen.AgentProviderConfig{
		Id:        p.ID,
		Provider:  p.Provider,
		Model:     p.Model,
		BaseUrl:   p.BaseURL,
		Enabled:   p.Enabled,
		HasKey:    p.HasKey,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func toAgentPolicyDTO(p agent.Policy) apigen.AgentPolicy {
	return apigen.AgentPolicy{
		AllowModels:          p.AllowModels,
		ForceAdminModels:     p.ForceAdminModels,
		MaxMessagesPerWindow: int(p.MaxMessagesPerWindow),
		WindowSeconds:        int(p.WindowSeconds),
		SpendCapCredits:      int(p.SpendCapCredits),
		DenyTools:            p.DenyTools,
	}
}

func policyFromDTO(p apigen.AgentPolicy) agent.Policy {
	return agent.Policy{
		AllowModels:          p.AllowModels,
		ForceAdminModels:     p.ForceAdminModels,
		MaxMessagesPerWindow: int32(p.MaxMessagesPerWindow),
		WindowSeconds:        int32(p.WindowSeconds),
		SpendCapCredits:      int64(p.SpendCapCredits),
		DenyTools:            p.DenyTools,
	}
}

// jsonObj unmarshals a jsonb column into a generic map for the DTO. A null or
// empty blob becomes an empty object so the response always matches the
// schema's `additionalProperties: true`.
func jsonObj(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// jsonResult mirrors jsonObj for the result column; kept separate so a failed
// parse is obviously non-fatal (the result blob is opaque agent output).
func jsonResult(raw json.RawMessage) map[string]any { return jsonObj(raw) }
