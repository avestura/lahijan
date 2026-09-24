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
// permissions on /auth/me. Aligns with the slug strings in
// internal/app/lahijan/auth/rbac/permissions.go. The scope-prefix check
// (`${scope}.admin`) at the call site still catches anything missing here.
const ROLE_IMPLICATIONS: Record<string, Set<string>> = {
  owner: new Set<unknown>(["*"]) as Set<string>,
  admin: new Set<unknown>(["*"]) as Set<string>,
  member: new Set<unknown>([
    // Compute.
    "compute.read",
    "compute.instance.create",
    "compute.instance.start",
    "compute.instance.stop",
    "compute.instance.restart",
    "compute.instance.delete",
    // WS-24: graphical (noVNC) console, distinct from xterm.js exec.
    "compute.instance.console.vnc",
    // DNS.
    "dns.zone.create",
    "dns.zone.read",
    "dns.zone.update",
    "dns.zone.delete",
    "dns.record.create",
    "dns.record.read",
    "dns.record.update",
    "dns.record.delete",
    // Object storage.
    "s3.bucket.create",
    "s3.bucket.read",
    "s3.bucket.update",
    "s3.bucket.delete",
    "s3.object.read",
    "s3.object.delete",
    "s3.credentials.create",
    "s3.credentials.revoke",
    // Billing (user-side).
    "billing.balance.read",
    "billing.ledger.read",
    "billing.receipt.read",
    "billing.receipt.create",
    "billing.price_catalog.read",
    // Audit (own tenant).
    "audit.read",
    // Plugins (read only).
    "plugins.read",
    // Agent chat (WS-31). Matches the backend member grants: full chat
    // surface + own BYOK providers, but NOT tenant policy (admin-only).
    "agent.conversation.create",
    "agent.conversation.read",
    "agent.conversation.delete",
    "agent.message.send",
    "agent.tool.confirm",
    "agent.provider.manage",
  ]) as Set<string>,
  viewer: new Set<unknown>([
    "compute.read",
    "dns.zone.read",
    "dns.record.read",
    "s3.bucket.read",
    "s3.object.read",
    "billing.balance.read",
    "billing.ledger.read",
    "billing.receipt.read",
    "billing.price_catalog.read",
    "audit.read",
    "plugins.read",
    // Agent chat (WS-31): viewer can read history only.
    "agent.conversation.read",
  ]) as Set<string>,
};

// The backend sends tenant role slugs as "tenant.owner" / "tenant.member" /
// ... (auth/rbac/roles.go); the table above is keyed by the bare role.
const TENANT_ROLE_PREFIX = "tenant.";

function rolesImply(roles: string[], perm: string): boolean {
  for (const r of roles) {
    if (r === PLATFORM_ADMIN) return true;
    const key = r.startsWith(TENANT_ROLE_PREFIX) ? r.slice(TENANT_ROLE_PREFIX.length) : r;
    const set = ROLE_IMPLICATIONS[key];
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
