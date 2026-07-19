/**
 * usePerm — client-side permission gate.
 *
 * Returns `{ hasPerm, isLoading }` for a given `scope.action` slug. Used to
 * hide/disable UI triggers that the user can't actually invoke. The server
 * remains the source of truth — this hook is defense in depth (per
 * web/AGENTS.md).
 *
 * Permission model (per docs/glossary.md):
 *   - Platform-wide role `platform.admin` implies every permission.
 *   - Tenant roles (`owner`, `admin`, `member`, `viewer`) imply progressively
 *     smaller permission sets. For now we map them heuristically; once
 *     /api/v1/me/permissions lands we'll consume the explicit list.
 *
 * Convention: the slug is `scope.action` (e.g. `compute.instance.create`).
 */
import { useSessionStore } from "./stores/session-store";

export interface UsePermResult {
  hasPerm: boolean;
  isLoading: boolean;
}

const PLATFORM_ADMIN = "platform.admin";

// Coarse role → scope.grant mapping used until the backend exposes explicit
// permissions on /auth/me. Adjust as ADR-0002 + WS-08 land a richer model.
const ROLE_IMPLICATIONS: Record<string, Set<string>> = {
  owner: new Set<unknown>(["*"]) as Set<string>,
  admin: new Set<unknown>(["*"]) as Set<string>,
  member: new Set<unknown>([
    "compute.read",
    "compute.instance.create",
    "compute.instance.start",
    "compute.instance.stop",
    "compute.instance.restart",
    "dns.read",
    "dns.zone.create",
    "dns.record.create",
    "storage.read",
    "storage.bucket.create",
    "billing.read",
    "audit.read",
    "plugins.read",
  ]) as Set<string>,
  viewer: new Set<unknown>([
    "compute.read",
    "dns.read",
    "storage.read",
    "billing.read",
    "audit.read",
    "plugins.read",
  ]) as Set<string>,
};

function rolesImply(roles: string[], perm: string): boolean {
  for (const r of roles) {
    if (r === PLATFORM_ADMIN) return true;
    const set = ROLE_IMPLICATIONS[r];
    if (!set) continue;
    if (set.has("*")) return true;
    if (set.has(perm)) return true;
    // Scope admins (e.g. "compute.admin") imply everything in their scope.
    const scope = perm.split(".")[0];
    if (scope && set.has(`${scope}.admin`)) return true;
  }
  return false;
}

/**
 * rolesImply is exported for tests + non-React callers (e.g. middleware).
 * React consumers should use the {@link usePerm} hook instead.
 */
export { rolesImply };

export function usePerm(perm: string): UsePermResult {
  const user = useSessionStore((s) => s.user);
  const roles = useSessionStore((s) => s.roles);
  const status = useSessionStore((s) => s.status);

  if (status === "loading") return { hasPerm: false, isLoading: true };
  if (!user) return { hasPerm: false, isLoading: false };
  return { hasPerm: rolesImply(roles, perm), isLoading: false };
}
