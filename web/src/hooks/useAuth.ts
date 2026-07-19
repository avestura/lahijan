/**
 * useAuth — selector around the session store + bootstrap query.
 *
 * Components that need to know "are we signed in?" read from this hook
 * rather than the store directly so we have one place to evolve when the
 * session shape grows (e.g. adding a /me/permissions fetch later).
 */
import { useBootstrapSession } from "@/features/auth/api";
import { useSessionStore } from "@/lib/stores/session-store";

export function useAuth() {
  const query = useBootstrapSession();
  const user = useSessionStore((s) => s.user);
  const status = useSessionStore((s) => s.status);
  const roles = useSessionStore((s) => s.roles);
  const currentTenantId = useSessionStore((s) => s.currentTenantId);

  return {
    user,
    status,
    roles,
    currentTenantId,
    isLoading: query.isLoading || status === "loading",
    isAuthenticated: !!user && status === "authenticated",
  };
}
