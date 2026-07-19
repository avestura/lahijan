/**
 * Auth query hooks (login, logout, current user).
 *
 * All mutations invalidate the appropriate query keys via the factory in
 * lib/api/keys. The login mutation populates the session store on success;
 * the logout mutation clears it.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useSessionStore } from "@/lib/stores/session-store";
import type { LoginValues } from "./schemas";

/**
 * useBootstrapSession — called once on app startup. Verifies the cookie
 * session is still good and populates the store from /auth/me.
 */
export function useBootstrapSession() {
  const setUser = useSessionStore((s) => s.setUser);

  return useQuery({
    queryKey: queryKeys.me(),
    queryFn: async () => {
      const { data, error, response } = await apiClient.GET("/api/v1/auth/me");
      if (error || !data) {
        if (response?.status === 401) {
          setUser(null);
        }
        throw new Error(`auth.me: ${response?.status ?? "unknown"}`);
      }
      setUser(data);
      return data;
    },
    retry: false,
    staleTime: 5 * 60_000,
    meta: { isBootstrap: true },
  });
}

/**
 * useSignIn — email + password login.
 *
 * On success the response carries the User object; cookies are set by the
 * backend HttpOnly. We populate the store directly so the UI flips to the
 * dashboard without an extra round-trip.
 */
export function useSignIn() {
  const queryClient = useQueryClient();
  const setUser = useSessionStore((s) => s.setUser);

  return useMutation({
    mutationFn: async (values: LoginValues) => {
      const { data, error, response } = await apiClient.POST("/api/v1/auth/login", {
        body: values,
      });
      if (response?.status === 202 && data) {
        // MFA challenge path; throw a typed error the form can render.
        const pending = (data as { pendingSessionToken?: string }).pendingSessionToken ?? "";
        throw new MFARequiredError(pending);
      }
      if (error || !data) {
        throw new Error(`auth.login: ${response?.status ?? "network"}`);
      }
      // Status 200 — narrow to the AuthResponse shape.
      const authed = data as { user: components["schemas"]["User"] };
      return authed;
    },
    onSuccess: (data) => {
      setUser(data.user);
      void queryClient.invalidateQueries({ queryKey: queryKeys.me() });
    },
  });
}

export class MFARequiredError extends Error {
  constructor(readonly pendingToken: string) {
    super("auth.login.mfaRequired");
    this.name = "MFARequiredError";
  }
}

/**
 * useSignOut — clears the server-side session + refresh cookies and resets
 * the local store. Idempotent.
 */
export function useSignOut() {
  const queryClient = useQueryClient();
  const reset = useSessionStore((s) => s.reset);

  return useMutation({
    mutationFn: async () => {
      const { error } = await apiClient.POST("/api/v1/auth/logout", {});
      if (error) {
        // Even if the server call failed we still reset locally so the UI
        // flips to the login page; the cookie is most likely already gone.
        return;
      }
    },
    onSettled: () => {
      reset();
      void queryClient.clear();
    },
  });
}
