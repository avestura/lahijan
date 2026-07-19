/**
 * Typed Lahijan API client singleton.
 *
 * This is the ONLY sanctioned surface for hitting the backend from the
 * frontend (per ADR-0015 + web/AGENTS.md). It wraps the auto-generated
 * `createLahijanClient` from `api/gen/ts-client` with:
 *
 *   - The base URL from `VITE_API_BASE_URL` (defaults to same-origin "" so
 *     the production SPA hits the host that served it).
 *   - The refresh middleware that auto-rotates the session cookie on 401.
 *
 * Direct `fetch` against `/api/v1/*` is discouraged outside of the refresh
 * middleware itself (which needs to bypass the client wrapper to avoid
 * infinite recursion).
 */
import { createLahijanClient, type paths } from "@api";

import { refreshMiddleware } from "./refresh-middleware";

const baseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

export type { paths };

/**
 * apiClient is the shared openapi-fetch instance for the dashboard. Every
 * feature imports from here, never from `@api` directly.
 *
 * @example
 *   const { data, error } = await apiClient.GET("/api/v1/ping", {});
 */
export const apiClient = createLahijanClient({
  baseUrl,
  middleware: [refreshMiddleware],
});

export type ApiClient = typeof apiClient;
