/**
 * Helpers for inspecting TanStack Query errors that originate from the
 * generated API client (`apiClient.GET`/`POST`). The query functions in
 * features/<area>/api.ts throw `Error` instances whose message carries
 * the HTTP status code as `"<scope>: <status>"`. These helpers parse
 * that shape without forcing callers to duplicate regexes.
 */

/**
 * Pattern that matches the convention used by every queryFn in
 * features/<area>/api.ts:
 *   throw new Error(`compute.instances.list: ${response?.status ?? "network"}`)
 * Captures the trailing status token (a number or the literal "network").
 */
const ERROR_STATUS_PATTERN = /:\s*(\d+|network)\s*$/i;

/** Extracts the HTTP status carried by a queryFn error, or null if absent. */
export function httpStatusFromError(err: unknown): number | "network" | null {
  if (!err || typeof err !== "object" || !("message" in err)) return null;
  const raw = (err as { message?: unknown }).message;
  if (typeof raw !== "string") return null;
  const match = ERROR_STATUS_PATTERN.exec(raw);
  const tok = match?.[1];
  if (!tok) return null;
  if (tok === "network") return "network";
  const n = Number.parseInt(tok, 10);
  return Number.isFinite(n) ? n : null;
}

/**
 * True when the error represents a backend "feature disabled" response
 * (HTTP 501). Use this to swap the ErrorState placeholder for the
 * friendlier FeatureDisabledState.
 */
export function isFeatureDisabledError(err: unknown): boolean {
  return httpStatusFromError(err) === 501;
}
