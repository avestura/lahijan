/**
 * Settings query + mutation hooks.
 *
 * Covers profile (PATCH /me), identities, MFA TOTP, WebAuthn, recovery
 * codes, and personal access tokens. Sessions list/revoke is not
 * exposed by the backend yet (the spec doesn't list /me/sessions
 * endpoints); we surface a localized "coming soon" notice in the UI.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";
import { useSessionStore } from "@/lib/stores/session-store";
import type {
  CreateTokenValues,
  DisableTotpValues,
  UpdateProfileValues,
  VerifyTotpValues,
} from "./schemas";

type PersonalAccessToken = components["schemas"]["PersonalAccessToken"];
type ExternalIdentity = components["schemas"]["ExternalIdentity"];
type TOTPEnrollResponse = components["schemas"]["TOTPEnrollResponse"];
type RecoveryCodesBatch = components["schemas"]["RecoveryCodesBatch"];
type RecoveryCodesListResponse = components["schemas"]["RecoveryCodesListResponse"];

// ---------------------------------------------------------------------------
// Profile
// ---------------------------------------------------------------------------

/** useUpdateProfile — PATCH /me. Refreshes the session store on success. */
export function useUpdateProfile() {
  const qc = useQueryClient();
  const setUser = useSessionStore((s) => s.setUser);
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: UpdateProfileValues) => {
      const { data, error, response } = await apiClient.PATCH("/api/v1/auth/me", {
        body: values,
      });
      if (error || !data) {
        throw new Error(`auth.me.patch: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: (user) => {
      setUser(user);
      void qc.invalidateQueries({ queryKey: queryKeys.me() });
      toast({ title: t("toast.saved"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Identities
// ---------------------------------------------------------------------------

export function useIdentities() {
  return useQuery({
    queryKey: queryKeys.my.identities(),
    queryFn: async (): Promise<ExternalIdentity[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/identities");
      if (error || !data) {
        throw new Error(`me.identities.list: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

export function useUnlinkIdentity() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ identityId }: { identityId: string }) => {
      const { error, response } = await apiClient.DELETE("/api/v1/me/identities/{identityId}", {
        params: { path: { identityId } },
      });
      if (error) {
        throw new Error(`me.identities.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.identities() });
      toast({ title: t("toast.deleted"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// MFA — TOTP
// ---------------------------------------------------------------------------

export function useEnrollTOTP() {
  return useMutation({
    mutationFn: async (): Promise<TOTPEnrollResponse> => {
      const { data, error, response } = await apiClient.POST("/api/v1/me/mfa/totp/enroll");
      if (error || !data) {
        throw new Error(`me.mfa.totp.enroll: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

export function useVerifyTOTP() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (values: VerifyTotpValues): Promise<RecoveryCodesBatch> => {
      const { data, error, response } = await apiClient.POST("/api/v1/me/mfa/totp/verify", {
        body: { code: values.code },
      });
      if (error || !data) {
        throw new Error(`me.mfa.totp.verify: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.recovery() });
    },
  });
}

export function useDisableTOTP() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: DisableTotpValues) => {
      const { error, response } = await apiClient.POST("/api/v1/me/mfa/totp/disable", {
        body: { currentPassword: values.currentPassword },
      });
      if (error) {
        throw new Error(`me.mfa.totp.disable: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.recovery() });
      toast({ title: t("toast.saved"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// MFA — recovery codes
// ---------------------------------------------------------------------------

export function useRecoveryCodesMeta() {
  return useQuery({
    queryKey: queryKeys.my.recovery(),
    queryFn: async (): Promise<RecoveryCodesListResponse> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/mfa/recovery");
      if (error || !data) {
        throw new Error(`me.mfa.recovery.list: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

export function useRegenerateRecoveryCodes() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<RecoveryCodesBatch> => {
      const { data, error, response } = await apiClient.POST("/api/v1/me/mfa/recovery");
      if (error || !data) {
        throw new Error(`me.mfa.recovery.regenerate: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.recovery() });
    },
  });
}

// ---------------------------------------------------------------------------
// MFA — WebAuthn
// ---------------------------------------------------------------------------

export function useAddWebAuthnCredential() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ name }: { name: string }) => {
      // Begin ceremony.
      const begin = await apiClient.POST("/api/v1/me/mfa/webauthn/register/begin", {
        body: { name },
      });
      if (begin.error || !begin.data) {
        throw new Error("me.mfa.webauthn.register.begin");
      }
      // The browser's WebAuthn API takes the publicKey options
      // verbatim. We rely on navigator.credentials being available.
      if (typeof navigator === "undefined" || !("credentials" in navigator)) {
        throw new Error("webauthn-unavailable");
      }
      const publicKey = begin.data.publicKey as unknown as PublicKeyCredentialCreationOptions;
      // The browser's WebAuthn API takes the publicKey options
      // verbatim; the backend pre-encodes challenge/user.id as
      // base64url strings. We hand the object through unchanged.
      const credential = (await navigator.credentials.create({
        publicKey,
      })) as PublicKeyCredential | null;
      if (!credential) throw new Error("webauthn-cancelled");
      // Finish ceremony.
      const response = credential.response as unknown;
      const finish = await apiClient.POST("/api/v1/me/mfa/webauthn/register/finish", {
        body: {
          session: begin.data.session,
          name,
          response: response as Record<string, unknown>,
        },
      });
      if (finish.error) {
        throw new Error("me.mfa.webauthn.register.finish");
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.identities() });
      toast({ title: t("toast.saved"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Personal access tokens
// ---------------------------------------------------------------------------

export function usePersonalAccessTokens() {
  return useQuery({
    queryKey: queryKeys.my.tokens(),
    queryFn: async (): Promise<PersonalAccessToken[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/auth/personal-access-tokens");
      if (error || !data) {
        throw new Error(`auth.pats.list: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

export function useCreatePersonalAccessToken() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (values: CreateTokenValues): Promise<PersonalAccessToken> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/auth/personal-access-tokens",
        {
          body: {
            name: values.name,
            scopes: values.scopes ?? [],
            expiresAt: values.expiresAt ?? null,
          },
        },
      );
      if (error || !data) {
        throw new Error(`auth.pats.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.tokens() });
    },
  });
}

export function useRevokePersonalAccessToken() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ tokenId }: { tokenId: string }) => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/auth/personal-access-tokens/{tokenId}",
        { params: { path: { tokenId } } },
      );
      if (error) {
        throw new Error(`auth.pats.revoke: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.my.tokens() });
      toast({ title: t("toast.deleted"), variant: "success" });
    },
  });
}
