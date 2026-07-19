/**
 * useTenant — selector + setter for the active tenant scope.
 *
 * The active tenant is sent as `X-Tenant-Id` on tenant-scoped requests (the
 * middleware will add it once WS-08's session-aware middleware is wired in
 * on the backend; for now we just track the state). The store persists the
 * user's pick in localStorage; if they only have one membership, we never
 * prompt and just use it.
 */
import { useSessionStore } from "@/lib/stores/session-store";

export function useTenant() {
  const user = useSessionStore((s) => s.user);
  const currentTenantId = useSessionStore((s) => s.currentTenantId);
  const setTenant = useSessionStore((s) => s.setTenant);

  const memberships = user?.memberships ?? [];

  return {
    memberships,
    currentTenantId,
    setTenant,
    hasMultipleTenants: memberships.length > 1,
  };
}
