/**
 * Object storage query + mutation hooks.
 *
 * All tenant-scoped queries carry the active tenant id in their query
 * key so the Header's tenant switcher transparently refetches them.
 *
 * Per WS-16: credential secret is shown exactly once at mint time
 * (mirrors the WS-06 PAT pattern). Quotas are enforced server-side;
 * the UI surfaces the cached usage + the configured quota.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";
import type {
  CreateBucketValues,
  CreateCredentialValues,
  PresignValues,
  QuotaValues,
  UpdateBucketValues,
} from "./schemas";

type StorageBucket = components["schemas"]["StorageBucket"];
type StorageCredential = components["schemas"]["StorageCredential"];
type StorageCredentialWithSecret =
  components["schemas"]["StorageCredentialWithSecret"];
type StorageBucketUsage = components["schemas"]["StorageBucketUsage"];
type StoragePresignResult = components["schemas"]["StoragePresignResult"];

// ---------------------------------------------------------------------------
// Buckets
// ---------------------------------------------------------------------------

/** useStorageBuckets — list of buckets in the active tenant. */
export function useStorageBuckets(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.storage.buckets(tenantId) : ["storage", "disabled"],
    enabled: !!tenantId,
    queryFn: async (): Promise<StorageBucket[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/storage/buckets", {});
      if (error || !data) {
        throw new Error(`storage.buckets.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useStorageBucket — single bucket. */
export function useStorageBucket(tenantId: string | null, bucketId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && bucketId ? queryKeys.storage.bucket(tenantId, bucketId) : ["storage", "disabled"],
    enabled: !!tenantId && !!bucketId,
    queryFn: async (): Promise<StorageBucket> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/storage/buckets/{bucketId}",
        { params: { path: { bucketId: bucketId! } } },
      );
      if (error || !data) {
        throw new Error(`storage.bucket.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useCreateStorageBucket — POST /buckets. */
export function useCreateStorageBucket(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: CreateBucketValues): Promise<StorageBucket> => {
      const { data, error, response } = await apiClient.POST("/api/v1/storage/buckets", {
        body: values,
      });
      if (error || !data) {
        throw new Error(`storage.bucket.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.storage.buckets(tenantId) });
      }
      toast({ title: t("storage.mutations.bucketCreateSuccess"), variant: "success" });
    },
  });
}

/** useUpdateStorageBucket — PATCH /buckets/{id}. */
export function useUpdateStorageBucket(tenantId: string | null, bucketId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: UpdateBucketValues) => {
      const { data, error, response } = await apiClient.PATCH(
        "/api/v1/storage/buckets/{bucketId}",
        { params: { path: { bucketId: bucketId! } }, body: values },
      );
      if (error || !data) {
        throw new Error(`storage.bucket.update: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && bucketId) {
        void qc.invalidateQueries({ queryKey: queryKeys.storage.bucket(tenantId, bucketId) });
      }
      toast({ title: t("storage.mutations.bucketUpdateSuccess"), variant: "success" });
    },
  });
}

/** useDeleteStorageBucket — DELETE /buckets/{id}. */
export function useDeleteStorageBucket(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ bucketId }: { bucketId: string }) => {
      const { error, response } = await apiClient.DELETE("/api/v1/storage/buckets/{bucketId}", {
        params: { path: { bucketId } },
      });
      if (error) {
        throw new Error(`storage.bucket.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.storage.buckets(tenantId) });
      }
      toast({ title: t("storage.mutations.bucketDeleteSuccess"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Usage + quota
// ---------------------------------------------------------------------------

/** useStorageBucketUsage — GET /buckets/{id}/usage. */
export function useStorageBucketUsage(tenantId: string | null, bucketId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && bucketId
        ? queryKeys.storage.usage(tenantId, bucketId)
        : ["storage", "disabled"],
    enabled: !!tenantId && !!bucketId,
    staleTime: 60_000,
    queryFn: async (): Promise<StorageBucketUsage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/storage/buckets/{bucketId}/usage",
        { params: { path: { bucketId: bucketId! } } },
      );
      if (error || !data) {
        throw new Error(`storage.bucket.usage: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useSetStorageBucketQuota — POST /buckets/{id}/quota. */
export function useSetStorageBucketQuota(tenantId: string | null, bucketId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: QuotaValues) => {
      const { error, response } = await apiClient.POST(
        "/api/v1/storage/buckets/{bucketId}/quota",
        {
          params: { path: { bucketId: bucketId! } },
          body: { quotaBytes: values.quotaBytes, quotaObjects: values.quotaObjects },
        },
      );
      if (error) {
        throw new Error(`storage.bucket.quota: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId && bucketId) {
        void qc.invalidateQueries({ queryKey: queryKeys.storage.bucket(tenantId, bucketId) });
        void qc.invalidateQueries({ queryKey: queryKeys.storage.usage(tenantId, bucketId) });
      }
      toast({ title: t("storage.mutations.quotaSuccess"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

/** useStorageCredentials — list credentials scoped to a bucket. */
export function useStorageCredentials(tenantId: string | null, bucketId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && bucketId
        ? queryKeys.storage.credentials(tenantId, bucketId)
        : ["storage", "disabled"],
    enabled: !!tenantId && !!bucketId,
    queryFn: async (): Promise<StorageCredential[]> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/storage/buckets/{bucketId}/credentials",
        { params: { path: { bucketId: bucketId! } } },
      );
      if (error || !data) {
        throw new Error(`storage.credentials.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useCreateStorageCredential — POST /buckets/{id}/credentials. */
export function useCreateStorageCredential(tenantId: string | null, bucketId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: CreateCredentialValues): Promise<StorageCredentialWithSecret> => {
      const body: components["schemas"]["StorageCredentialCreateRequest"] = {
        actions: values.actions,
        label: values.label,
      };
      if (values.expiresInSeconds) body.expiresInSeconds = values.expiresInSeconds;
      const { data, error, response } = await apiClient.POST(
        "/api/v1/storage/buckets/{bucketId}/credentials",
        { params: { path: { bucketId: bucketId! } }, body },
      );
      if (error || !data) {
        throw new Error(`storage.credentials.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId && bucketId) {
        void qc.invalidateQueries({
          queryKey: queryKeys.storage.credentials(tenantId, bucketId),
        });
      }
      toast({ title: t("storage.mutations.credentialCreateSuccess"), variant: "success" });
    },
  });
}

/** useRevokeStorageCredential — DELETE /buckets/{id}/credentials/{cid}. */
export function useRevokeStorageCredential(tenantId: string | null, bucketId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ credentialId }: { credentialId: string }) => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/storage/buckets/{bucketId}/credentials/{credentialId}",
        { params: { path: { bucketId: bucketId!, credentialId } } },
      );
      if (error) {
        throw new Error(`storage.credentials.revoke: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId && bucketId) {
        void qc.invalidateQueries({
          queryKey: queryKeys.storage.credentials(tenantId, bucketId),
        });
      }
      toast({ title: t("storage.mutations.credentialRevokeSuccess"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Presign
// ---------------------------------------------------------------------------

/** usePresignStorageObject — POST /buckets/{id}/presign. */
export function usePresignStorageObject(bucketId: string | undefined) {
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: PresignValues): Promise<StoragePresignResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/storage/buckets/{bucketId}/presign",
        {
          params: { path: { bucketId: bucketId! } },
          body: {
            method: values.method,
            key: values.key,
            expiresInSeconds: values.expiresInSeconds,
          },
        },
      );
      if (error || !data) {
        throw new Error(`storage.presign: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      toast({ title: t("storage.mutations.presignSuccess"), variant: "success" });
    },
  });
}
