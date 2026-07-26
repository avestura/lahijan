/**
 * AgentChat — the WS-31 agent chat surface.
 *
 * Two panes: a conversation list (left) and the active conversation (right)
 * with a streaming message view + composer. Destructive tool calls pause the
 * turn and surface a confirm modal (human-in-the-loop); approve/decline hits
 * the /tool-calls/{id}/confirm endpoint.
 *
 * The turn itself is streamed via the raw-fetch helper in ./api (SSE); every
 * other call goes through the typed openapi-fetch client.
 *
 * The whole feature degrades to a "not enabled" panel when the backend
 * returns 501 (agent.enabled=false).
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangleIcon, MessageSquarePlusIcon, SendIcon, Trash2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { isFeatureDisabledError } from "@/lib/api-errors";
import {
  useAgentConversation,
  useAgentConversations,
  useConfirmAgentToolCall,
  useCreateAgentConversation,
  useDeleteAgentConversation,
  streamAgentMessage,
  type AgentMessage,
  type PendingToolCall,
} from "@/features/agent/api";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/lib/api/keys";

export function AgentChat() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;

  const convos = useAgentConversations(tenantId);
  const [activeId, setActiveId] = useState<string | null>(null);
  const detail = useAgentConversation(tenantId, activeId ?? undefined);

  const canCreate = usePerm("agent.conversation.create");
  const canSend = usePerm("agent.message.send");

  // When the user deletes their active conversation we keep them on the
  // "Select or start a conversation" placeholder instead of auto-jumping to
  // another thread. This ref suppresses the first-load auto-select in that case.
  const suppressedAutoSelect = useRef(false);

  // Auto-select the newest conversation when the list first loads so the
  // right pane is not empty. Skipped after a delete so the placeholder shows.
  useEffect(() => {
    if (suppressedAutoSelect.current) return;
    if (!activeId && convos.data && convos.data.length > 0) {
      const first = convos.data[0];
      if (first) setActiveId(first.id);
    }
  }, [activeId, convos.data]);

  // Clear the selection when the active conversation is deleted so the right
  // pane falls back to the placeholder rather than rendering a stale/404 thread.
  function handleDeleted(id: string) {
    if (id === activeId) {
      suppressedAutoSelect.current = true;
      setActiveId(null);
    }
  }

  if (convos.isError && isFeatureDisabledError(convos.error)) {
    return <DisabledPanel label={t("agent.disabled")} />;
  }

  return (
    <div className="flex h-full min-h-0">
      <ConversationSidebar
        activeId={activeId}
        onSelect={setActiveId}
        onDeleted={handleDeleted}
        canCreate={canCreate.hasPerm}
      />
      <ConversationPane
        key={activeId ?? "none"}
        conversationId={activeId}
        canSend={canSend.hasPerm}
        // When the detail query errors with 501 the feature is disabled.
        disabled={detail.isError && isFeatureDisabledError(detail.error)}
        messages={detail.data?.messages ?? []}
        tenantId={tenantId}
      />
    </div>
  );
}

/** Left pane: list of conversations + a "new" button. */
function ConversationSidebar(props: {
  activeId: string | null;
  onSelect: (id: string) => void;
  onDeleted: (id: string) => void;
  canCreate: boolean;
}) {
  const { t } = useTranslation();
  const tenant = useTenant();
  const convos = useAgentConversations(tenant.currentTenantId);
  const create = useCreateAgentConversation(tenant.currentTenantId);
  const del = useDeleteAgentConversation(tenant.currentTenantId);

  return (
    <div className="flex w-64 shrink-0 flex-col border-e border-border bg-card/40">
      <div className="flex items-center justify-between gap-2 p-3">
        <span className="text-sm font-semibold">{t("agent.conversations")}</span>
        <Button
          size="sm"
          variant="outline"
          disabled={!props.canCreate || create.isPending}
          onClick={() =>
            create.mutate(undefined, {
              onSuccess: (c) => props.onSelect(c.id),
            })
          }
          aria-label={t("agent.newConversation")}
        >
          <MessageSquarePlusIcon className="h-4 w-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto px-2 pb-2">
        {convos.isLoading && (
          <div className="p-3 text-sm text-muted-foreground">{t("common.loading")}</div>
        )}
        {!convos.isLoading && (convos.data?.length ?? 0) === 0 && (
          <div className="p-3 text-sm text-muted-foreground">{t("agent.noConversations")}</div>
        )}
        {convos.data?.map((c) => (
          <div
            key={c.id}
            className={
              "group flex cursor-pointer items-center justify-between gap-2 rounded-md px-3 py-2 text-sm transition-colors " +
              (c.id === props.activeId ? "bg-accent text-accent-foreground" : "hover:bg-accent/50")
            }
            onClick={() => props.onSelect(c.id)}
          >
            <span className="truncate">{c.title || t("agent.untitled")}</span>
            <button
              type="button"
              className="opacity-0 transition-opacity group-hover:opacity-100"
              aria-label={t("agent.deleteConversation")}
              onClick={(e) => {
                e.stopPropagation();
                del.mutate(c.id, { onSuccess: () => props.onDeleted(c.id) });
              }}
            >
              <Trash2Icon className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Right pane: the active conversation with a streaming message view + composer. */
function ConversationPane(props: {
  conversationId: string | null;
  canSend: boolean;
  disabled: boolean;
  messages: AgentMessage[];
  tenantId: string | null;
}) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [input, setInput] = useState("");
  // Live messages: seeded from the persisted query, updated optimistically
  // while a turn streams, then re-synced when the query refetches.
  const [live, setLive] = useState<AgentMessage[]>(props.messages);
  const [streaming, setStreaming] = useState(false);
  const [pending, setPending] = useState<PendingToolCall | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const confirm = useConfirmAgentToolCall(props.tenantId, props.conversationId ?? undefined);

  useEffect(() => {
    setLive(props.messages);
  }, [props.messages]);

  // Keep the message list scrolled to the bottom as content streams in.
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [live, streaming, pending]);

  // Abort any in-flight stream on unmount / conversation switch.
  useEffect(() => {
    return () => abortRef.current?.abort();
  }, [props.conversationId]);

  async function send() {
    if (!props.conversationId || !props.tenantId || !input.trim() || streaming) return;
    const text = input.trim();
    setInput("");
    // Optimistic: user message + an empty assistant bubble that the stream fills.
    const userMsg: AgentMessage = {
      id: `local-${Date.now()}-u`,
      role: "user",
      content: text,
      createdAt: new Date().toISOString(),
    };
    const assistantMsg: AgentMessage = {
      id: `local-${Date.now()}-a`,
      role: "assistant",
      content: "",
      createdAt: new Date().toISOString(),
    };
    setLive((prev) => [...prev, userMsg, assistantMsg]);
    setStreaming(true);

    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await streamAgentMessage({
        tenantId: props.tenantId,
        conversationId: props.conversationId,
        message: text,
        signal: controller.signal,
        onEvent: (ev) => {
          if (ev.type === "text") {
            setLive((prev) => {
              const next = [...prev];
              const last = next[next.length - 1];
              if (last && last.role === "assistant") {
                next[next.length - 1] = { ...last, content: last.content + ev.text };
              }
              return next;
            });
          } else if (ev.type === "tool_call" && ev.tool_call_id) {
            setPending({
              toolCallId: ev.tool_call_id,
              tool: ev.tool,
              args: ev.args,
            });
          } else if (ev.type === "error") {
            // The backend surfaced a failure (no provider configured, the
            // model returned an error, a policy blocked the call, ...).
            // Surface it in the assistant bubble so the user sees WHY the
            // turn produced no reply instead of an empty "Thinking…" state.
            const why = ev.error ?? "unknown error";
            setLive((prev) => {
              const next = [...prev];
              const last = next[next.length - 1];
              if (last && last.role === "assistant") {
                const prefix = last.content ? last.content + "\n\n" : "";
                next[next.length - 1] = { ...last, content: prefix + "⚠️ " + why };
              }
              return next;
            });
          } else if (ev.type === "done") {
            // Refetch so the persisted (server-truth) messages replace the
            // optimistic bubbles, capturing the finalized assistant content
            // + any tool-call rows.
            if (props.tenantId && props.conversationId) {
              void qc.invalidateQueries({
                queryKey: queryKeys.agent.conversation(props.tenantId, props.conversationId),
              });
            }
          }
        },
      });
    } finally {
      setStreaming(false);
      abortRef.current = null;
    }
  }

  if (props.disabled) {
    return <DisabledPanel label={t("agent.disabled")} />;
  }

  if (!props.conversationId) {
    return (
      <div className="flex flex-1 items-center justify-center p-8 text-center text-sm text-muted-foreground">
        {t("agent.selectConversation")}
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <div ref={scrollRef} className="flex-1 space-y-4 overflow-y-auto p-6">
        {live.map((m) => (
          <MessageBubble
            key={m.id}
            message={m}
            streaming={streaming && m === live[live.length - 1]}
          />
        ))}
      </div>
      <div className="border-t border-border p-3">
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={t("agent.composerPlaceholder")}
            disabled={!props.canSend || streaming}
            aria-label={t("agent.composerLabel")}
          />
          <Button type="submit" disabled={!props.canSend || streaming || !input.trim()} size="icon">
            <SendIcon className="h-4 w-4 rtl:-scale-x-100" />
          </Button>
        </form>
        {!props.canSend && (
          <p className="mt-1 text-xs text-muted-foreground">{t("agent.noSendPermission")}</p>
        )}
      </div>

      {pending && (
        <ConfirmModal
          pending={pending}
          onCancel={() => setPending(null)}
          onResolved={() => setPending(null)}
          confirm={confirm}
        />
      )}
    </div>
  );
}

/** A single chat bubble. Assistant bubbles align to the start; user to the end. */
function MessageBubble(props: { message: AgentMessage; streaming: boolean }) {
  const { t } = useTranslation();
  const isUser = props.message.role === "user";
  return (
    <div className={"flex " + (isUser ? "justify-end" : "justify-start")}>
      <div
        className={
          "max-w-[80%] whitespace-pre-wrap rounded-lg px-4 py-2 text-sm " +
          (isUser ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground")
        }
      >
        {props.message.content || (props.streaming ? t("agent.thinking") : "")}
      </div>
    </div>
  );
}

/** Human-in-the-loop confirmation modal for a pending destructive tool call. */
function ConfirmModal(props: {
  pending: PendingToolCall;
  onCancel: () => void;
  onResolved: () => void;
  confirm: ReturnType<typeof useConfirmAgentToolCall>;
}) {
  const { t } = useTranslation();
  const argPreview = useMemo(() => {
    try {
      return props.pending.args ? JSON.stringify(props.pending.args, null, 2) : "";
    } catch {
      return "";
    }
  }, [props.pending.args]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-lg">
        <div className="mb-2 flex items-center gap-2 font-semibold text-destructive">
          <AlertTriangleIcon className="h-5 w-5" />
          {t("agent.confirmTitle")}
        </div>
        <p className="mb-3 text-sm text-muted-foreground">
          {t("agent.confirmBody", { tool: props.pending.tool })}
        </p>
        {argPreview && (
          <pre className="mb-3 max-h-40 overflow-auto rounded-md bg-muted p-2 text-xs">
            {argPreview}
          </pre>
        )}
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            disabled={props.confirm.isPending}
            onClick={() => {
              props.confirm.mutate(
                { toolCallId: props.pending.toolCallId, approved: false },
                { onSettled: props.onResolved },
              );
            }}
          >
            {t("agent.decline")}
          </Button>
          <Button
            variant="destructive"
            disabled={props.confirm.isPending}
            onClick={() => {
              props.confirm.mutate(
                { toolCallId: props.pending.toolCallId, approved: true },
                { onSettled: props.onResolved },
              );
            }}
          >
            {t("agent.approve")}
          </Button>
        </div>
      </div>
    </div>
  );
}

function DisabledPanel({ label }: { label: string }) {
  return (
    <div className="flex flex-1 items-center justify-center p-8">
      <div className="max-w-sm rounded-lg border border-border bg-card p-6 text-center text-sm text-muted-foreground">
        {label}
      </div>
    </div>
  );
}
