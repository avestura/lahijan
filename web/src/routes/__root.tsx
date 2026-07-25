/**
 * Root route. Carries the providers that must wrap every page:
 *   - QueryClientProvider (TanStack Query)
 *   - Suspense (so react-i18next can suspend until the bundle is ready)
 *
 * Also wraps every non-`/login` route in AppShell. The original layout
 * design assumed all auth-required routes would live under `_auth/`,
 * but only `_auth/dashboard.tsx` actually does — every other page
 * (`/settings`, `/compute`, `/billing`, `/audit`, ...) is a direct
 * child of root and was rendering without the sidebar + header. Doing
 * the wrap here keeps the file structure intact.
 *
 * TanStack Router file-based routing auto-discovers this file. The plugin
 * regenerates `src/routeTree.gen.ts` on dev / build.
 */
import { createRootRoute, Outlet, useRouterState } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { Suspense } from "react";

import { queryClient } from "@/lib/queryClient";
import { AppShell } from "@/components/layout/AppShell";
import { NotFoundState } from "@/components/layout/NotFoundState";

export const Route = createRootRoute({
  component: RootComponent,
  notFoundComponent: NotFoundState,
});

function RootComponent() {
  // Render the auth layout (sidebar + header + bootstrap query) for every
  // route EXCEPT the standalone ones (login right now). The router state
  // is read at render time so navigation between layouts works without a
  // full reload.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isStandalone = pathname === "/login";

  return (
    <QueryClientProvider client={queryClient}>
      <Suspense fallback={null}>
        {isStandalone ? <Outlet /> : <AppShell />}
      </Suspense>
    </QueryClientProvider>
  );
}
