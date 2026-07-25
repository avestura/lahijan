/**
 * `_auth` — pathless layout route for authenticated pages.
 *
 * Note: AppShell (sidebar + header + bootstrap query) is rendered by the
 * ROOT route for every non-`/login` URL, so this layout is now a pure
 * pass-through. Kept as a placeholder so the file-based router still
 * recognises `_auth/*` routes (e.g. `_auth/dashboard.tsx`) and so future
 * cross-cutting auth concerns (e.g. MFA re-prompt, IP allowlist) have a
 * single place to live.
 */
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { useSessionStore } from "@/lib/stores/session-store";

export const Route = createFileRoute("/_auth")({
  beforeLoad: () => {
    const { user, status } = useSessionStore.getState();
    // If we know for sure the session is gone, bounce to /login. If status
    // is still "loading", let the route render; AppShell will react when
    // the bootstrap resolves.
    if (!user && status === "anonymous") {
      // TanStack Router's beforeLoad uses thrown `redirect()` to navigate.
      // eslint-disable-next-line @typescript-eslint/only-throw-error
      throw redirect({ to: "/login" });
    }
  },
  component: AuthLayout,
});

function AuthLayout() {
  return <Outlet />;
}
