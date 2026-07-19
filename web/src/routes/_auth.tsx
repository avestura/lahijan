/**
 * `_auth` — pathless layout route for authenticated pages.
 *
 * beforeLoad checks the session store; if no user is loaded yet AND we
 * haven't tried to bootstrap, we let the route load (the bootstrap query
 * will populate the store, and the route's content will react to the
 * result). If a refresh failure has already cleared the session, we
 * redirect to /login.
 */
import { createFileRoute, redirect } from "@tanstack/react-router";

import { AppShell } from "@/components/layout/AppShell";
import { useSessionStore } from "@/lib/stores/session-store";

export const Route = createFileRoute("/_auth")({
  beforeLoad: () => {
    const { user, status } = useSessionStore.getState();
    // If we know for sure the session is gone, bounce to /login. If status
    // is still "loading", let the route render; the AppShell will react
    // when the bootstrap resolves.
    if (!user && status === "anonymous") {
      // TanStack Router's beforeLoad uses thrown `redirect()` to navigate.
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({ to: "/login" });
    }
  },
  component: AuthLayout,
});

function AuthLayout() {
  return <AppShell />;
}
