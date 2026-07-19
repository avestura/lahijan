/**
 * `/` — root index route.
 *
 * Redirects to /dashboard when authenticated, /login otherwise. We don't
 * render anything here; the redirect happens before paint.
 */
import { createFileRoute, redirect } from "@tanstack/react-router";

import { useSessionStore } from "@/lib/stores/session-store";

export const Route = createFileRoute("/")({
  beforeLoad: () => {
    const { user } = useSessionStore.getState();
    // eslint-disable-next-line @typescript-eslint/only-throw-error
    throw redirect({ to: user ? "/dashboard" : "/login" });
  },
});
