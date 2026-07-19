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
 */
import { Outlet } from "@tanstack/react-router";

import { Header } from "./Header";
import { Sidebar } from "./Sidebar";
import { Toaster } from "@/components/ui/toaster";

export function AppShell() {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-background text-foreground">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-y-auto">
          <div className="container mx-auto max-w-7xl p-6">
            <Outlet />
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  );
}
