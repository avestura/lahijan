/**
 * Auth + tenancy session store.
 *
 * Holds the authenticated user, the active tenant, and the role/permission
 * cache. Populated by the auth bootstrap (which GETs /api/v1/auth/me on app
 * load) and by the login mutation. Cleared on logout / 401-on-refresh.
 *
 * The store persists only the `currentTenantId` to localStorage — auth
 * itself is cookie-based and should never be persisted in JS-visible
 * storage.
 */
import { create } from "zustand";
import { persist } from "zustand/middleware";

import type { components } from "@api-schema";

export type User = components["schemas"]["User"];
export type Membership = components["schemas"]["Membership"];

export type SessionStatus = "anonymous" | "authenticated" | "loading";

const TENANT_STORAGE_KEY = "lahijan.activeTenantId";

interface SessionState {
  user: User | null;
  status: SessionStatus;
  /** Active tenant scope; sent via X-Tenant-Id on tenant-scoped requests. */
  currentTenantId: string | null;
  /** Roles held in the active tenant (best-effort; the server enforces too). */
  roles: string[];

  setUser: (user: User | null) => void;
  setStatus: (status: SessionStatus) => void;
  setTenant: (tenantId: string | null) => void;
  /**
   * Re-derive the role list from the current user's memberships against the
   * active tenant. Called whenever user or tenant changes.
   */
  refreshRoles: () => void;
  /** Clear everything; called on logout and on refresh failure. */
  reset: () => void;
}

export const useSessionStore = create<SessionState>()(
  persist(
    (set, get) => ({
      user: null,
      status: "loading",
      currentTenantId: null,
      roles: [],

      setUser: (user) => {
        const currentTenantId = get().currentTenantId ?? user?.memberships?.[0]?.tenantId ?? null;
        set({ user, currentTenantId });
        get().refreshRoles();
        set({ status: user ? "authenticated" : "anonymous" });
      },

      setStatus: (status) => set({ status }),

      setTenant: (tenantId) => {
        set({ currentTenantId: tenantId });
        get().refreshRoles();
      },

      refreshRoles: () => {
        const { user, currentTenantId } = get();
        if (!user || !currentTenantId) {
          set({ roles: [] });
          return;
        }
        const roles = (user.memberships ?? [])
          .filter((m) => m.tenantId === currentTenantId)
          .map((m) => m.role);
        set({ roles });
      },

      reset: () => {
        set({
          user: null,
          status: "anonymous",
          currentTenantId: null,
          roles: [],
        });
      },
    }),
    {
      name: TENANT_STORAGE_KEY,
      // Only persist the active tenant id — user/auth must not live in
      // JS-readable storage. The bootstrap call to /auth/me rehydrates user.
      partialize: (s) => ({ currentTenantId: s.currentTenantId }),
      onRehydrateStorage: () => (state) => {
        // Roles can't be re-derived without the user; clear them and let
        // the bootstrap re-populate after /auth/me returns.
        if (state) state.roles = [];
      },
    },
  ),
);

/**
 * clearSession is exported for use from non-React modules (the refresh
 * middleware, mostly). React consumers should use `useSessionStore`.
 */
export function clearSession(): void {
  useSessionStore.getState().reset();
}
