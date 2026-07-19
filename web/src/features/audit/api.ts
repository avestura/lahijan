/**
 * Audit query + export hooks.
 *
 * Tenant-scoped at the repository layer; the middleware sets the
 * tenant context, queries filter automatically.
 */
import { useQuery } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";

type AuditEvent = components["schemas"]["AuditEvent"];

/**
 * AuditFilters — the shape of the audit list query params. Mirrors
 * the API's filter surface (minus the pagination pieces, which are
 * first-class on the hook).
 */
export interface AuditFilters {
  actorUserId?: string;
  action?: string;
  resourceType?: string;
  status?: "success" | "failure" | "pending";
  actorType?: "user" | "system" | "plugin";
  fromTs?: string;
  toTs?: string;
}

/** useAuditEvents — paginated audit list with filters. */
export function useAuditEvents(filters: AuditFilters, offset = 0, limit = 50) {
  return useQuery({
    queryKey: queryKeys.audit.list({ ...filters, offset, limit }),
    staleTime: 30_000,
    queryFn: async (): Promise<{ items: AuditEvent[]; total: number }> => {
      const { data, error, response } = await apiClient.GET("/api/v1/audit", {
        params: {
          query: {
            offset,
            limit,
            actorUserId: filters.actorUserId,
            action: filters.action,
            resourceType: filters.resourceType,
            status: filters.status,
            actorType: filters.actorType,
            fromTs: filters.fromTs,
            toTs: filters.toTs,
          },
        },
      });
      if (error || !data) {
        throw new Error(`audit.list: ${response?.status ?? "network"}`);
      }
      return { items: data.items, total: data.total };
    },
  });
}

/** useAuditEvent — single audit event by id. */
export function useAuditEvent(auditId: string | undefined) {
  return useQuery({
    queryKey: auditId ? queryKeys.audit.detail(auditId) : ["audit", "disabled"],
    enabled: !!auditId,
    queryFn: async (): Promise<AuditEvent> => {
      const { data, error, response } = await apiClient.GET("/api/v1/audit/{auditId}", {
        params: { path: { auditId: auditId! } },
      });
      if (error || !data) {
        throw new Error(`audit.detail: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/**
 * buildAuditExportURL — compose the audit export URL with the same
 * filters as the list. Returns a URL that can be opened directly
 * (window.open or anchor download) — the response is text/csv or
 * application/json, and the cookie auth is sent automatically.
 */
export function buildAuditExportURL(format: "csv" | "json", filters: AuditFilters): string {
  const params = new URLSearchParams();
  params.set("format", format);
  for (const [k, v] of Object.entries(filters)) {
    if (v !== undefined && v !== "") params.set(k, String(v));
  }
  return `/api/v1/audit/export?${params.toString()}`;
}
