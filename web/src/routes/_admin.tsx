/**
 * `_admin` — pathless layout route for platform-admin pages.
 *
 * Layered on top of `_auth` (admin pages are also auth-guarded). The
 * beforeLoad checks the user's roles in the session store; if they don't
 * hold `platform.admin`, we redirect to /dashboard with an error message.
 */
import { createFileRoute, redirect } from "@tanstack/react-router";

import { AppShell } from "@/components/layout/AppShell";
import { useSessionStore } from "@/lib/stores/session-store";

const PLATFORM_ADMIN = "platform.admin";

export const Route = createFileRoute("/_admin")({
  beforeLoad: () => {
    const { user, roles, status } = useSessionStore.getState();
    if (!user && status === "anonymous") {
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({ to: "/login" });
    }
    if (user && !roles.includes(PLATFORM_ADMIN)) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({ to: "/dashboard" });
    }
  },
  component: AdminLayout,
});

function AdminLayout() {
  return <AppShell />;
}
