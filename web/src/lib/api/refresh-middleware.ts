/**
 * Auth + refresh middleware for the openapi-fetch client.
 *
 * The Lahijan backend authenticates with an HttpOnly session cookie plus a
 * refresh-token cookie (set at /login and rotated at /refresh). Because the
 * session cookie is HttpOnly, JS cannot read it; we rely on `credentials:
 * "include"` on every request, and on this middleware to:
 *
 *   1. Catch 401 responses from non-auth endpoints.
 *   2. POST /api/v1/auth/refresh once to obtain a fresh session.
 *   3. Retry the original request exactly one time.
 *
 * Concurrent 401s share a single in-flight refresh promise so the refresh
 * endpoint is only hit once per wave.
 *
 * If the refresh fails (or the response itself is from the refresh
 * endpoint), the session is considered dead: the session store is cleared
 * and we redirect to /login.
 */
import type { Middleware } from "openapi-fetch";

import { clearSession, useSessionStore } from "../stores/session-store";

const REFRESH_PATH = "/api/v1/auth/refresh";
const LOGIN_PATH = "/api/v1/auth/login";
const LOGOUT_PATH = "/api/v1/auth/logout";
const TENANT_HEADER = "X-Tenant-Id";

let inflightRefresh: Promise<boolean> | null = null;

async function doRefresh(): Promise<boolean> {
  try {
    const res = await fetch(REFRESH_PATH, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    return res.ok;
  } catch {
    return false;
  }
}

/**
 * refreshMiddleware stamps the active tenant id (read from the session
 * store) on every outbound request and retries 401s once after a
 * successful refresh.
 *
 * The tenant header is what the backend's `tenant` middleware reads to
 * scope the query at the repository layer; without it, tenant-scoped
 * endpoints respond 400 ("a tenant scope is required"). Auth endpoints
 * (login, refresh, logout) are intentionally tenant-agnostic and skip
 * the header.
 *
 * The retry piece is the runtime companion to the TanStack Query `retry`
 * setting: queries will retry, but only after a successful refresh;
 * otherwise they bubble to the caller.
 */
export const refreshMiddleware: Middleware = {
  onRequest({ request }) {
    // Make sure every request carries the HttpOnly cookies.
    request.headers.set("credentials", "include");
    // Stamp the active tenant so the backend scopes the request. The
    // auth endpoints don't need it (and login runs before the user has
    // picked a tenant), so we skip them explicitly.
    const url = new URL(request.url);
    const isAuthPath =
      url.pathname === REFRESH_PATH || url.pathname === LOGIN_PATH || url.pathname === LOGOUT_PATH;
    if (!isAuthPath) {
      const tenantId = useSessionStore.getState().currentTenantId;
      if (tenantId) {
        request.headers.set(TENANT_HEADER, tenantId);
      }
    }
    return request;
  },
  async onResponse({ request, response }) {
    if (response.status !== 401) return response;
    const url = new URL(request.url);
    // Don't recurse on the auth endpoints themselves.
    if (
      url.pathname === REFRESH_PATH ||
      url.pathname === LOGIN_PATH ||
      url.pathname === LOGOUT_PATH
    ) {
      return response;
    }
    if (!inflightRefresh) {
      inflightRefresh = doRefresh().finally(() => {
        inflightRefresh = null;
      });
    }
    const ok = await inflightRefresh;
    if (!ok) {
      clearSession();
      // The router picks the cleared-session state up via the session store
      // subscription and redirects to /login.
      return response;
    }
    // Retry the original request once.
    const headers = new Headers(request.headers);
    return fetch(request.url, {
      method: request.method,
      headers,
      body: request.body,
      credentials: "include",
      // @ts-expect-error -- duplex is a valid fetch option in browsers for
      // streamed request bodies; not yet in the lib.dom types.
      duplex: "half",
    });
  },
};
