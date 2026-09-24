/**
 * DNS query + mutation hooks.
 *
 * All tenant-scoped queries carry the active tenant id in their query
 * key so the Header's tenant switcher transparently refetches them
 * when the user changes scope.
 *
 * Per WS-15: every privileged action emits an audit event server-side;
 * the WS-21 UI is the surface for those actions.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { apiErrorMessage } from "@/lib/api-errors";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";
import type { CreateRecordValues, CreateZoneValues, UpdateRecordValues } from "./schemas";

type DNSZone = components["schemas"]["DNSZone"];
type DNSRecord = components["schemas"]["DNSRecord"];
type DNSTemplate = components["schemas"]["DNSTemplate"];
type ApplyDNSTemplateResult = components["schemas"]["ApplyDNSTemplateResult"];

// ---------------------------------------------------------------------------
// Zones
// ---------------------------------------------------------------------------

/**
 * useDNSZones — list of zones in the active tenant.
 */
export function useDNSZones(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.dns.zones(tenantId) : ["dns", "disabled"],
    enabled: !!tenantId,
    queryFn: async (): Promise<DNSZone[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/dns/zones", {});
      if (error || !data) {
        throw new Error(`dns.zones.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useDNSZone — single zone; reconciled on read by the backend. */
export function useDNSZone(tenantId: string | null, zoneId: string | undefined) {
  return useQuery({
    queryKey: tenantId && zoneId ? queryKeys.dns.zone(tenantId, zoneId) : ["dns", "disabled"],
    enabled: !!tenantId && !!zoneId,
    queryFn: async (): Promise<DNSZone> => {
      const { data, error, response } = await apiClient.GET("/api/v1/dns/zones/{zoneId}", {
        params: { path: { zoneId: zoneId! } },
      });
      if (error || !data) {
        throw new Error(`dns.zone.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useCreateDNSZone — POST /zones; invalidates the zone list. */
export function useCreateDNSZone(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (
      values: CreateZoneValues,
    ): Promise<{ zone: DNSZone; applyTemplateId?: string }> => {
      const { data, error } = await apiClient.POST("/api/v1/dns/zones", {
        body: {
          name: values.name,
          description: values.description,
          kind: values.kind,
        },
      });
      if (error || !data) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.zoneCreateError")));
      }
      return { zone: data, applyTemplateId: values.templateId };
    },
    onSuccess: async ({ zone, applyTemplateId }) => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.zones(tenantId) });
      }
      // Apply the template (if any) after the zone exists. Failures here
      // don't undo the create; the toast still says "zone created" and
      // the user can re-apply from the zone's Templates tab.
      if (applyTemplateId) {
        try {
          await apiClient.POST("/api/v1/dns/zones/{zoneId}/apply-template", {
            params: { path: { zoneId: zone.id } },
            body: { templateId: applyTemplateId },
          });
          void qc.invalidateQueries({
            queryKey: queryKeys.dns.records(tenantId!, zone.id),
          });
        } catch {
          /* surface below */
        }
      }
      toast({ title: t("dns.mutations.zoneCreateSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

/** useUpdateDNSZone — PATCH /zones/{id}. */
export function useUpdateDNSZone(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: { description?: string; kind?: "Native" | "Master" | "Slave" }) => {
      const { data, error } = await apiClient.PATCH("/api/v1/dns/zones/{zoneId}", {
        params: { path: { zoneId: zoneId! } },
        body: values,
      });
      if (error || !data) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.zoneUpdateError")));
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.zone(tenantId, zoneId) });
      }
      toast({ title: t("dns.mutations.zoneUpdateSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

/** useDeleteDNSZone — DELETE /zones/{id}. */
export function useDeleteDNSZone(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ zoneId }: { zoneId: string }) => {
      const { error } = await apiClient.DELETE("/api/v1/dns/zones/{zoneId}", {
        params: { path: { zoneId } },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.zoneDeleteError")));
      }
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.zones(tenantId) });
      }
      toast({ title: t("dns.mutations.zoneDeleteSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

/** useSetDNSSEC — POST /zones/{id}/dnssec/{enable|disable}. */
export function useSetDNSSEC(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ action }: { action: "enable" | "disable" }) => {
      const { error } = await apiClient.POST("/api/v1/dns/zones/{zoneId}/dnssec/{action}", {
        params: { path: { zoneId: zoneId!, action } },
      });
      if (error) {
        throw new Error(
          apiErrorMessage(
            error,
            t(
              action === "enable"
                ? "dns.mutations.dnssecEnableError"
                : "dns.mutations.dnssecDisableError",
            ),
          ),
        );
      }
    },
    onSuccess: (_d, vars) => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.zone(tenantId, zoneId) });
      }
      toast({
        title:
          vars.action === "enable"
            ? t("dns.mutations.dnssecEnableSuccess")
            : t("dns.mutations.dnssecDisableSuccess"),
        variant: "success",
      });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/**
 * useDNSRecords — list of records in a zone.
 *
 * No polling: the records table changes less frequently than the
 * compute instance state and the user can refresh on demand.
 */
export function useDNSRecords(tenantId: string | null, zoneId: string | undefined) {
  return useQuery({
    queryKey: tenantId && zoneId ? queryKeys.dns.records(tenantId, zoneId) : ["dns", "disabled"],
    enabled: !!tenantId && !!zoneId,
    queryFn: async (): Promise<DNSRecord[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/dns/zones/{zoneId}/records", {
        params: { path: { zoneId: zoneId! } },
      });
      if (error || !data) {
        throw new Error(`dns.records.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useCreateDNSRecord — POST /zones/{id}/records. */
export function useCreateDNSRecord(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: CreateRecordValues): Promise<DNSRecord> => {
      const { data, error } = await apiClient.POST("/api/v1/dns/zones/{zoneId}/records", {
        params: { path: { zoneId: zoneId! } },
        body: {
          name: values.name,
          type: values.type,
          content: values.content,
          ttl: values.ttl,
          disabled: values.disabled,
        },
      });
      if (error || !data) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.recordCreateError")));
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.records(tenantId, zoneId) });
      }
      toast({ title: t("dns.mutations.recordCreateSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

/** useUpdateDNSRecord — PATCH /zones/{id}/records/{rid}. */
export function useUpdateDNSRecord(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({
      recordId,
      values,
    }: {
      recordId: string;
      values: UpdateRecordValues;
    }): Promise<DNSRecord> => {
      const { data, error } = await apiClient.PATCH(
        "/api/v1/dns/zones/{zoneId}/records/{recordId}",
        {
          params: { path: { zoneId: zoneId!, recordId } },
          body: {
            content: values.content,
            ttl: values.ttl,
            disabled: values.disabled,
          },
        },
      );
      if (error || !data) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.recordUpdateError")));
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.records(tenantId, zoneId) });
      }
      toast({ title: t("dns.mutations.recordUpdateSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

/** useDeleteDNSRecord — DELETE /zones/{id}/records/{rid}. */
export function useDeleteDNSRecord(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ recordId }: { recordId: string }) => {
      const { error } = await apiClient.DELETE("/api/v1/dns/zones/{zoneId}/records/{recordId}", {
        params: { path: { zoneId: zoneId!, recordId } },
      });
      if (error) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.recordDeleteError")));
      }
    },
    onSuccess: () => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.records(tenantId, zoneId) });
      }
      toast({ title: t("dns.mutations.recordDeleteSuccess"), variant: "success" });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

/** useDNSTemplates — GET /dns/templates. */
export function useDNSTemplates() {
  return useQuery({
    queryKey: queryKeys.dns.templates(),
    staleTime: 5 * 60_000,
    queryFn: async (): Promise<DNSTemplate[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/dns/templates");
      if (error || !data) {
        throw new Error(`dns.templates.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useApplyDNSTemplate — POST /zones/{id}/apply-template. */
export function useApplyDNSTemplate(tenantId: string | null, zoneId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ templateId }: { templateId: string }): Promise<ApplyDNSTemplateResult> => {
      const { data, error } = await apiClient.POST("/api/v1/dns/zones/{zoneId}/apply-template", {
        params: { path: { zoneId: zoneId! } },
        body: { templateId },
      });
      if (error || !data) {
        throw new Error(apiErrorMessage(error, t("dns.mutations.templateApplyError")));
      }
      return data;
    },
    onSuccess: (result) => {
      if (tenantId && zoneId) {
        void qc.invalidateQueries({ queryKey: queryKeys.dns.records(tenantId, zoneId) });
      }
      toast({
        title: t("dns.mutations.templateApplySuccess"),
        description: t("dns.templates.applied", { count: result.applied }),
        variant: "success",
      });
    },
    onError: (err) => {
      toast({ title: err.message, variant: "destructive" });
    },
  });
}
