/**
 * TanStack Query client (singleton).
 *
 * - 60s stale time matches the dashboard's "good enough" freshness window
 *   for most reads; specific queries override via `staleTime` per-hook.
 * - No retry on 401 (the refresh middleware in lib/api handles that).
 * - Network mode "always" so the dev-server proxy + production same-origin
 *   paths behave identically.
 */
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      gcTime: 5 * 60_000,
      retry: (failureCount, error) => {
        // Never retry auth errors — the refresh middleware already had its
        // chance; retrying here just delays the bounce to /login.
        if (error instanceof Error && error.message.includes("401")) {
          return false;
        }
        return failureCount < 2;
      },
      refetchOnWindowFocus: false,
      networkMode: "always",
    },
    mutations: {
      networkMode: "always",
    },
  },
});

export type { QueryClient };
