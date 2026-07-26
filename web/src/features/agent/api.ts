/**
 * Agent chat query + mutation hooks (WS-31).
 *
 * Conversation/message/provider/policy state lives in TanStack Query like
 * every other module. The one exception is the streamed agent turn
 * (POST /conversations/{id}/messages): it returns text/event-stream, which
 * openapi-fetch cannot consume (it parses a single JSON body). For that one
 * endpoint we drop down to a raw `fetch` that mirrors the auth/tenant headers
 * the refresh middleware stamps on every other request, and parse the SSE
 * frames by hand. This is the sanctioned exception to the "no direct fetch"
 * rule.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";

type AgentConversation = components["schemas"]["AgentConversation"];
type AgentMessage = components["schemas"]["AgentMessage"];
type AgentToolCall = components["schemas"]["AgentToolCall"];
type AgentConversationDetail = components["schemas"]["AgentConversationDetail"];
type AgentProviderConfig = components["schemas"]["AgentProviderConfig"];
type AgentPolicy = components["schemas"]["AgentPolicy"];

/** Discriminated union of the SSE events the agent turn streams back. */
export type AgentStreamEvent =
  | { type: "text"; text: string }
  | { type: "tool_call"; tool: string; args?: Record<string, unknown>; tool_call_id?: string }
  | { type: "tool_result"; tool: string; tool_call_id?: string; result?: Record<string, unknown> }
  | { type: "done" }
  | { type: "error"; error?: string };

/** A pending destructive tool call surfaced to the UI for confirmation. */
export interface PendingToolCall {
  toolCallId: string;
  tool: string;
  args?: Record<string, unknown>;
}

/** useAgentConversations — the caller's conversation list, newest first. */
export function useAgentConversations(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.agent.conversations(tenantId) : ["agent", "disabled"],
    enabled: !!tenantId,
    queryFn: async (): Promise<AgentConversation[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/agent/conversations", {});
      if (error || !data) {
        throw new Error(`agent.conversations.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useAgentConversation — one conversation with its message + tool-call history. */
export function useAgentConversation(tenantId: string | null, conversationId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && conversationId
        ? queryKeys.agent.conversation(tenantId, conversationId)
        : ["agent", "detail", "disabled"],
    enabled: !!tenantId && !!conversationId,
    queryFn: async (): Promise<AgentConversationDetail> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/agent/conversations/{conversationId}",
        { params: { path: { conversationId: conversationId! } } },
      );
      if (error || !data) {
        throw new Error(`agent.conversation.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useCreateAgentConversation — start a new conversation. */
export function useCreateAgentConversation(tenantId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (title?: string): Promise<AgentConversation> => {
      const { data, error, response } = await apiClient.POST("/api/v1/agent/conversations", {
        body: title ? { title } : {},
      });
      if (error || !data) {
        throw new Error(`agent.conversation.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId)
        void qc.invalidateQueries({ queryKey: queryKeys.agent.conversations(tenantId) });
    },
  });
}

/** useDeleteAgentConversation — delete a conversation + its history. */
export function useDeleteAgentConversation(tenantId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (conversationId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/agent/conversations/{conversationId}",
        { params: { path: { conversationId } } },
      );
      if (error) {
        throw new Error(`agent.conversation.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId)
        void qc.invalidateQueries({ queryKey: queryKeys.agent.conversations(tenantId) });
    },
  });
}

/** useConfirmAgentToolCall — approve or decline a pending (destructive) tool call. */
export function useConfirmAgentToolCall(tenantId: string | null, conversationId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: { toolCallId: string; approved: boolean }): Promise<AgentToolCall> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/agent/tool-calls/{toolCallId}/confirm",
        { params: { path: { toolCallId: args.toolCallId } }, body: { approved: args.approved } },
      );
      if (error || !data) {
        throw new Error(`agent.tool.confirm: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && conversationId) {
        void qc.invalidateQueries({
          queryKey: queryKeys.agent.conversation(tenantId, conversationId),
        });
      }
    },
  });
}

/** useAgentProviders — the caller's BYOK provider configs (keys redacted). */
export function useAgentProviders(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.agent.providers(tenantId) : ["agent", "providers", "disabled"],
    enabled: !!tenantId,
    queryFn: async (): Promise<AgentProviderConfig[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/agent/providers", {});
      if (error || !data) {
        throw new Error(`agent.providers.list: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useSaveAgentProvider — add a BYOK provider config. */
export function useSaveAgentProvider(tenantId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: {
      provider: string;
      apiKey: string;
      model?: string;
      baseUrl?: string;
    }): Promise<AgentProviderConfig> => {
      const { data, error, response } = await apiClient.POST("/api/v1/agent/providers", {
        body: {
          provider: input.provider,
          apiKey: input.apiKey,
          model: input.model,
          baseUrl: input.baseUrl,
        },
      });
      if (error || !data) {
        throw new Error(`agent.providers.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId) void qc.invalidateQueries({ queryKey: queryKeys.agent.providers(tenantId) });
    },
  });
}

/** useDeleteAgentProvider — remove a BYOK provider config. */
export function useDeleteAgentProvider(tenantId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (providerId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE("/api/v1/agent/providers/{providerId}", {
        params: { path: { providerId } },
      });
      if (error) {
        throw new Error(`agent.providers.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId) void qc.invalidateQueries({ queryKey: queryKeys.agent.providers(tenantId) });
    },
  });
}

/** useAgentPolicy — the tenant's agent policy (permissive default when unset). */
export function useAgentPolicy(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.agent.policy(tenantId) : ["agent", "policy", "disabled"],
    enabled: !!tenantId,
    queryFn: async (): Promise<AgentPolicy> => {
      const { data, error, response } = await apiClient.GET("/api/v1/agent/policy", {});
      if (error || !data) {
        throw new Error(`agent.policy.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useUpdateAgentPolicy — create or replace the tenant's agent policy. */
export function useUpdateAgentPolicy(tenantId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (policy: AgentPolicy): Promise<AgentPolicy> => {
      const { data, error, response } = await apiClient.PUT("/api/v1/agent/policy", {
        body: policy,
      });
      if (error || !data) {
        throw new Error(`agent.policy.update: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId) void qc.invalidateQueries({ queryKey: queryKeys.agent.policy(tenantId) });
    },
  });
}

/**
 * streamAgentMessage runs one agent turn and invokes `onEvent` for each SSE
 * frame the backend streams back. Returns when the turn is done (a `done`
 * event) or the server closes the stream. The caller wires `onEvent` to its
 * React state; `signal` lets the component abort an in-flight turn on
 * unmount or cancel.
 *
 * This is the ONE raw-fetch site in the agent feature: openapi-fetch buffers
 * the whole body as JSON, which defeats streaming. We mirror the refresh
 * middleware's headers (cookies + X-Tenant-Id) so the request authenticates
 * + scopes exactly like every typed call.
 */
export async function streamAgentMessage(args: {
  tenantId: string;
  conversationId: string;
  message: string;
  signal?: AbortSignal;
  onEvent: (event: AgentStreamEvent) => void;
}): Promise<void> {
  const res = await fetch(
    `/api/v1/agent/conversations/${encodeURIComponent(args.conversationId)}/messages`,
    {
      method: "POST",
      credentials: "include",
      signal: args.signal,
      headers: {
        "content-type": "application/json",
        "x-tenant-id": args.tenantId,
        accept: "text/event-stream",
      },
      body: JSON.stringify({ message: args.message }),
    },
  );
  if (!res.ok || !res.body) {
    let detail = `${res.status}`;
    try {
      const errBody = (await res.json()) as { error?: { message?: string } };
      detail = errBody?.error?.message ?? detail;
    } catch {
      /* keep status text */
    }
    args.onEvent({ type: "error", error: detail });
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    // SSE frames are separated by a blank line. Split + dispatch any
    // complete frames; keep the trailing partial in the buffer.
    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) >= 0) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      const line = frame.split("\n").find((l) => l.startsWith("data:"));
      if (!line) continue;
      const payload = line.slice(5).trim();
      if (!payload) continue;
      try {
        args.onEvent(JSON.parse(payload) as AgentStreamEvent);
      } catch {
        /* ignore malformed frame; the stream self-heals on the next one */
      }
    }
  }
}

/** AgentMessage role helper for the UI (type-narrowing). */
export function isUserMessage(m: AgentMessage): boolean {
  return m.role === "user";
}

export type {
  AgentConversation,
  AgentMessage,
  AgentToolCall,
  AgentConversationDetail,
  AgentProviderConfig,
  AgentPolicy,
};
