/**
 * AppShell — the dashboard's authenticated chrome.
 *
 * Composed of:
 *   - Sidebar (primary nav)
 *   - Header (toggles + user menu + tenant switcher + Command+K trigger)
 *   - <Outlet/> where the route's page renders
 *   - <Toaster/> for transient notifications
 *
 * Mounted by the auth-guarded layout route (src/routes/_auth.tsx). The admin
 * layout (src/routes/_admin.tsx) re-uses this shell and layers on the
 * admin-only nav.
 *
 * Calls useAuth() once on mount so the /auth/me bootstrap query fires
 * (TanStack Query dedupes, so multiple calls are cheap). Without this the
 * session store stays empty on a fresh page load even when the cookie is
 * valid — every page rendered user-null + tenant-null and the API calls
 * failed with tenant_scope_required.
 */
import { Outlet } from "@tanstack/react-router";

import { Header } from "./Header";
import { Sidebar } from "./Sidebar";
import { Toaster } from "@/components/ui/toaster";
import { useAuth } from "@/hooks/useAuth";

export function AppShell() {
  // Fire the bootstrap query (GET /api/v1/auth/me) once on mount. Result
  // flows into the session store via the query's setUser callback.
  useAuth();

  return (
    <div className="flex h-screen w-full overflow-hidden bg-canvas text-ink">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-y-auto bg-canvas">
          <div className="mx-auto w-full max-w-[var(--bx-container-wide)] p-6">
            <Outlet />
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  );
}
