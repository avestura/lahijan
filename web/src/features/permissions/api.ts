/**
 * Permission catalogue hook.
 *
 * GET /api/v1/permissions lists every permission slug with a description. It
 * powers checklists wherever a permission set is chosen (token scopes today).
 */
import { useQuery } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";

export type PermissionInfo = components["schemas"]["PermissionInfo"];

/** usePermissionCatalog — the permission catalogue, fetched only when enabled. */
export function usePermissionCatalog(enabled = true) {
  return useQuery({
    queryKey: queryKeys.permissions.catalog(),
    enabled,
    staleTime: 10 * 60_000,
    queryFn: async (): Promise<PermissionInfo[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/permissions", {});
      if (error || !data) {
        throw new Error(`permissions.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}
