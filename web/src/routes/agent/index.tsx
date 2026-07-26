/**
 * /agent — the AI agent chat page (WS-31).
 *
 * Renders the AgentChat feature component. The page relies on the __root
 * AppShell wrapper for the sidebar + auth bootstrap (same as /compute etc.),
 * so it lives at the top level rather than under _auth/. The component
 * itself degrades to a "not enabled" panel when the backend returns 501
 * (agent.enabled=false).
 */
import { createFileRoute } from "@tanstack/react-router";

import { AgentChat } from "@/features/agent/AgentChat";

export const Route = createFileRoute("/agent/")({
  component: AgentPage,
});

function AgentPage() {
  // AppShell wraps <Outlet/> in `container p-6` inside `main.overflow-y-auto`
  // (header is h-14 = 3.5rem, container padding is 3rem top+bottom). The chat
  // is a fixed-height flex layout (messages scroll, composer pinned), so it
  // must size itself to exactly the available viewport or the whole page
  // scrolls. dvh keeps mobile browser chrome honest.
  return (
    <div className="h-[calc(100dvh-6.5rem)] overflow-hidden">
      <AgentChat />
    </div>
  );
}
