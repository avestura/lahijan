/**
 * Root route. Carries the providers that must wrap every page:
 *   - QueryClientProvider (TanStack Query)
 *   - Suspense (so react-i18next can suspend until the bundle is ready)
 *
 * TanStack Router file-based routing auto-discovers this file. The plugin
 * regenerates `src/routeTree.gen.ts` on dev / build.
 */
import { createRootRoute, Outlet } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { Suspense } from "react";

import { queryClient } from "@/lib/queryClient";

export const Route = createRootRoute({
  component: RootComponent,
});

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <Suspense fallback={null}>
        <Outlet />
      </Suspense>
    </QueryClientProvider>
  );
}
