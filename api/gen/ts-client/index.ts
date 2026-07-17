/**
 * Lahijan public API - typed HTTP client (openapi-fetch based).
 *
 * This module is the single sanctioned client for the Lahijan frontend (web/
 * dashboard in WS-18 and website/ marketing site in WS-19). It wraps
 * `openapi-fetch` with the OpenAPI-derived `paths`/`components` types in
 * `../ts/schema.d.ts`, so every call is type-checked against the spec and the
 * types can never drift (ADR-0015). Raw `fetch` against the API is
 * discouraged.
 *
 * The client is framework-agnostic: callers supply a base URL. The dashboard
 * wires authentication (Authorization header / session cookie) via middleware
 * in WS-06/WS-18.
 */

import createClient, { type Middleware } from "openapi-fetch";

import type { components, operations, paths } from "../ts/schema";

// Re-export the generated types so consumers import everything from here.
export type { components, operations, paths };

/**
 * The standard error envelope, mirroring the server-side
 * `{error:{code,message,details?}}` shape defined in api/openapi.yaml.
 */
export type ErrorEnvelope = components["schemas"]["Error"];

export interface CreateLahijanClientOptions {
  /**
   * Base URL of the Lahijan backend, e.g. "https://api.lahijan.dev".
   * For same-origin usage from the dashboard, pass an empty string.
   */
  baseUrl: string;
  /** Default headers attached to every request (e.g. Authorization). */
  defaultHeaders?: Record<string, string>;
  /** Optional openapi-fetch middlewares (auth refresh, tracing, logging). */
  middleware?: Middleware[];
}

/**
 * createLahijanClient builds a typed openapi-fetch client for the Lahijan API.
 *
 * @example
 *   const client = createLahijanClient({ baseUrl: "/api" });
 *   const { data, error } = await client.GET("/api/v1/ping", {});
 */
export function createLahijanClient(
  opts: CreateLahijanClientOptions,
): ReturnType<typeof createClient<paths>> {
  const client = createClient<paths>({
    baseUrl: opts.baseUrl,
    headers: opts.defaultHeaders,
  });
  for (const m of opts.middleware ?? []) {
    client.use(m);
  }
  return client;
}
