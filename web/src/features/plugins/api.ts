/**
 * Plugins query + mutation hooks.
 *
 * Admin-only surface (the _admin layout guard enforces this). The
 * upload endpoint is multipart/form-data so we don't go through the
 * typed openapi-fetch wrapper for that one call — direct fetch via
 * apiClient's underlying client is OK because the path is fixed and
 * the response shape is documented.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";

type AdminPlugin = components["schemas"]["AdminPlugin"];
type AdminMarketplaceEntry = components["schemas"]["AdminMarketplaceEntry"];
type AdminPluginUpgradeResult = components["schemas"]["AdminPluginUpgradeResult"];

// ---------------------------------------------------------------------------
// Installed plugins
// ---------------------------------------------------------------------------

/** useAdminPlugins — GET /admin/plugins. */
export function useAdminPlugins() {
  return useQuery({
    queryKey: queryKeys.plugins.list(),
    staleTime: 30_000,
    queryFn: async (): Promise<AdminPlugin[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/plugins", {
        params: { query: { limit: 100 } },
      });
      if (error || !data) {
        throw new Error(`admin.plugins.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useAdminPlugin — GET /admin/plugins/{id}. */
export function useAdminPlugin(pluginId: string | undefined) {
  return useQuery({
    queryKey: pluginId ? queryKeys.plugins.detail(pluginId) : ["plugins", "disabled"],
    enabled: !!pluginId,
    queryFn: async (): Promise<AdminPlugin> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/plugins/{pluginId}", {
        params: { path: { pluginId: pluginId! } },
      });
      if (error || !data) {
        throw new Error(`admin.plugin.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/**
 * useUploadAdminPlugin — POST /admin/plugins/upload (multipart).
 *
 * Uses a direct fetch because openapi-fetch's typed wrapper doesn't
 * surface FormData bodies well in v0.13. The cookie auth is sent
 * automatically (credentials: 'include' is the openapi-fetch default).
 */
export function useUploadAdminPlugin() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({
      wasm,
      manifest,
    }: {
      wasm: File;
      manifest: string;
    }): Promise<AdminPlugin> => {
      const fd = new FormData();
      fd.append("wasm", wasm);
      fd.append("manifest", manifest);
      const resp = await fetch("/api/v1/admin/plugins/upload", {
        method: "POST",
        body: fd,
        credentials: "include",
      });
      if (!resp.ok) {
        throw new Error(`admin.plugins.upload: ${resp.status}`);
      }
      return (await resp.json()) as AdminPlugin;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      toast({ title: t("plugins.mutations.uploadSuccess"), variant: "success" });
    },
  });
}

/** useEnableAdminPlugin — POST /admin/plugins/{id}/enable. */
export function useEnableAdminPlugin() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ pluginId }: { pluginId: string }): Promise<AdminPlugin> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/plugins/{pluginId}/enable",
        { params: { path: { pluginId } } },
      );
      if (error || !data) {
        throw new Error(`admin.plugin.enable: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: (_d, vars) => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.detail(vars.pluginId) });
      toast({ title: t("plugins.mutations.enableSuccess"), variant: "success" });
    },
  });
}

/** useDisableAdminPlugin — POST /admin/plugins/{id}/disable. */
export function useDisableAdminPlugin() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ pluginId }: { pluginId: string }): Promise<AdminPlugin> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/plugins/{pluginId}/disable",
        { params: { path: { pluginId } } },
      );
      if (error || !data) {
        throw new Error(`admin.plugin.disable: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: (_d, vars) => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.detail(vars.pluginId) });
      toast({ title: t("plugins.mutations.disableSuccess"), variant: "success" });
    },
  });
}

/** useDeleteAdminPlugin — DELETE /admin/plugins/{id}. */
export function useDeleteAdminPlugin() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ pluginId }: { pluginId: string }) => {
      const { error, response } = await apiClient.DELETE("/api/v1/admin/plugins/{pluginId}", {
        params: { path: { pluginId } },
      });
      if (error) {
        throw new Error(`admin.plugin.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      toast({ title: t("plugins.mutations.deleteSuccess"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Permissions
// ---------------------------------------------------------------------------

/**
 * useSetAdminPluginPermission — POST /admin/plugins/{id}/permissions/{perm}/{action}.
 *
 * The permission slug is URL-encoded (colon → %3A) so the path
 * matches the route.
 */
export function useSetAdminPluginPermission() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({
      pluginId,
      permission,
      action,
    }: {
      pluginId: string;
      permission: string;
      action: "grant" | "revoke";
    }): Promise<string[]> => {
      const encoded = encodeURIComponent(permission);
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/plugins/{pluginId}/permissions/{permission}/{action}",
        { params: { path: { pluginId, permission: encoded, action } } },
      );
      if (error || !data) {
        throw new Error(`admin.plugin.permission: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: (_d, vars) => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.detail(vars.pluginId) });
      toast({
        title:
          vars.action === "grant"
            ? t("plugins.mutations.grantSuccess")
            : t("plugins.mutations.revokeSuccess"),
        variant: "success",
      });
    },
  });
}

// ---------------------------------------------------------------------------
// Marketplace
// ---------------------------------------------------------------------------

/** useAdminMarketplace — GET /admin/marketplace. */
export function useAdminMarketplace() {
  return useQuery({
    queryKey: queryKeys.plugins.marketplace(),
    staleTime: 5 * 60_000,
    queryFn: async (): Promise<AdminMarketplaceEntry[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/marketplace");
      if (error || !data) {
        throw new Error(`admin.marketplace.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useInstallFromMarketplace — POST /admin/plugins/install/{name}. */
export function useInstallFromMarketplace() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ name }: { name: string }): Promise<AdminPlugin> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/plugins/install/{name}",
        { params: { path: { name } } },
      );
      if (error || !data) {
        throw new Error(`admin.marketplace.install: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      toast({ title: t("plugins.mutations.installSuccess"), variant: "success" });
    },
  });
}

/** useUpgradeFromMarketplace — POST /admin/plugins/upgrade/{name}. */
export function useUpgradeFromMarketplace() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ name }: { name: string }): Promise<AdminPluginUpgradeResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/plugins/upgrade/{name}",
        { params: { path: { name } } },
      );
      if (error || !data) {
        throw new Error(`admin.marketplace.upgrade: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.plugins.list() });
      toast({ title: t("plugins.mutations.upgradeSuccess"), variant: "success" });
    },
  });
}
