/**
 * Compute query + mutation hooks.
 *
 * All tenant-scoped queries carry the active tenant id in their query
 * key, so the Header's tenant switcher transparently refetches them
 * when the user changes scope.
 *
 * The instance-detail query polls every 5 seconds while the instance
 * is in a transitional state (Starting / Stopping). This matches the
 * WS-20 doc's default answer to the "real-time" open question
 * (polling for MVP; WebSocket push lands in Phase 7).
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";
import type { CreateInstanceValues } from "./schemas";

type Instance = components["schemas"]["ComputeInstance"];
type ComputeImage = components["schemas"]["ComputeImage"];
type ComputeProfile = components["schemas"]["ComputeProfile"];
type ExecResult = components["schemas"]["ComputeExecResult"];

/** Lifecycle actions the API exposes via POST /instances/{id}/{action}. */
export type LifecycleAction = "start" | "stop" | "restart" | "freeze" | "unfreeze";

/**
 * useComputeInstances — list of instances in the active tenant.
 *
 * Polls every 5s so the user sees state transitions (start / stop) without
 * a manual refresh, per the WS-20 doc's "all status changes reflect
 * within 5s" DoD line.
 */
export function useComputeInstances(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.compute.instances(tenantId) : ["compute", "disabled"],
    enabled: !!tenantId,
    // Poll every 5s while the list is healthy. Stop polling once we hit
    // a 501 (feature disabled) — the operator has to flip a config flag
    // and restart Lahijan, so retrying every 5s just spams the log.
    refetchInterval: (query) => (isFeatureDisabledError(query.state.error) ? false : 5_000),
    queryFn: async (): Promise<Instance[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/compute/instances", {});
      if (error || !data) {
        throw new Error(`compute.instances.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useComputeInstance — single instance, polled every 5s while in transition. */
export function useComputeInstance(tenantId: string | null, instanceId: string | undefined) {
  return useQuery({
    queryKey:
      tenantId && instanceId
        ? queryKeys.compute.instance(tenantId, instanceId)
        : ["compute", "disabled"],
    enabled: !!tenantId && !!instanceId,
    refetchInterval: (query) => {
      const inst = query.state.data;
      if (!inst) return 5_000;
      const transitional =
        inst.status === "Starting" ||
        inst.status === "Stopping" ||
        inst.status === "Freezing" ||
        inst.status === "Unfreezing" ||
        inst.status === "Restarting";
      return transitional ? 2_000 : 10_000;
    },
    queryFn: async (): Promise<Instance> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/compute/instances/{instanceId}",
        { params: { path: { instanceId: instanceId! } } },
      );
      if (error || !data) {
        throw new Error(`compute.instance.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useComputeImages — featured + custom images for the create wizard. */
export function useComputeImages(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.compute.images(tenantId) : ["compute", "disabled"],
    enabled: !!tenantId,
    staleTime: 5 * 60_000, // image catalog changes rarely
    queryFn: async (): Promise<ComputeImage[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/compute/images", {});
      if (error || !data) {
        throw new Error(`compute.images.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useComputeProfiles — profiles available in the active tenant. */
export function useComputeProfiles(tenantId: string | null) {
  return useQuery({
    queryKey: tenantId ? queryKeys.compute.profiles(tenantId) : ["compute", "disabled"],
    enabled: !!tenantId,
    staleTime: 5 * 60_000,
    queryFn: async (): Promise<ComputeProfile[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/compute/profiles", {});
      if (error || !data) {
        throw new Error(`compute.profiles.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/**
 * buildCreateBody — translate the wizard form values into the API's
 * ComputeInstanceCreateRequest shape.
 *
 * We collect cpu/memory/disk as first-class numbers in the form (better
 * UX) and emit them as the `limits.cpu` / `limits.memory` / `limits.disk`
 * keys Incus expects in the `config` map.
 */
function buildCreateBody(
  values: CreateInstanceValues,
): components["schemas"]["ComputeInstanceCreateRequest"] {
  const config: Record<string, string> = {
    "limits.cpu": String(values.cpu),
    "limits.memory": `${values.memoryMiB}MiB`,
  };
  // The root disk size lives in devices.root.size rather than config.
  const devices: Record<string, Record<string, string>> = {
    root: { type: "disk", path: "/", size: `${values.diskGiB}GiB` },
  };
  return {
    name: values.name,
    type: values.type,
    imageAlias: values.imageAlias,
    description: values.description,
    config,
    devices,
    profiles: [values.profile],
  };
}

/** useCreateInstance — POST to /instances, invalidate the list on success. */
export function useCreateInstance(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: CreateInstanceValues): Promise<Instance> => {
      const { data, error, response } = await apiClient.POST("/api/v1/compute/instances", {
        body: buildCreateBody(values),
      });
      if (error || !data) {
        throw new Error(`compute.instance.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.compute.instances(tenantId) });
      }
      toast({ title: t("compute.mutations.createSuccess"), variant: "success" });
    },
  });
}

/** useDeleteInstance — DELETE /instances/{id}; force flag skips graceful stop. */
export function useDeleteInstance(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({ instanceId, force }: { instanceId: string; force?: boolean }) => {
      const { error, response } = await apiClient.DELETE("/api/v1/compute/instances/{instanceId}", {
        params: { path: { instanceId }, query: { force: force ?? false } },
      });
      if (error) {
        throw new Error(`compute.instance.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.compute.instances(tenantId) });
      }
      toast({ title: t("compute.mutations.deleteSuccess"), variant: "success" });
    },
  });
}

/** useLifecycle — POST /instances/{id}/{action}. */
export function useLifecycle(tenantId: string | null) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async ({
      instanceId,
      action,
      force,
    }: {
      instanceId: string;
      action: LifecycleAction;
      force?: boolean;
    }): Promise<Instance | null> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/compute/instances/{instanceId}/{action}",
        {
          params: {
            path: { instanceId, action },
            query: { force: force ?? false },
          },
        },
      );
      if (error || !data) {
        throw new Error(`compute.instance.${action}: ${response?.status ?? "network"}`);
      }
      return data ?? null;
    },
    onSuccess: (_data, vars) => {
      if (tenantId) {
        void qc.invalidateQueries({ queryKey: queryKeys.compute.instances(tenantId) });
        void qc.invalidateQueries({
          queryKey: queryKeys.compute.instance(tenantId, vars.instanceId),
        });
      }
      const key = `${vars.action}Success` as const;
      toast({ title: t(`compute.mutations.${key}`), variant: "success" });
    },
  });
}

/** useExecInstance — POST /instances/{id}/exec with a one-shot command. */
export function useExecInstance(instanceId: string | undefined) {
  return useMutation({
    mutationFn: async ({
      command,
      cwd,
    }: {
      command: string[];
      cwd?: string;
    }): Promise<ExecResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/compute/instances/{instanceId}/exec",
        {
          params: { path: { instanceId: instanceId! } },
          body: { command, cwd, user: 0, group: 0 },
        },
      );
      if (error || !data) {
        throw new Error(`compute.instance.exec: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/**
 * classifyStatus - map an Incus status string to the four buckets the
 * UI filter dropdown exposes. Returns "other" for anything unusual so
 * the user still sees it.
 */
export type StatusBucket = "running" | "stopped" | "frozen" | "other";

export function classifyStatus(status: string | undefined): StatusBucket {
  if (!status) return "other";
  const s = status.toLowerCase();
  // Incus' canonical status strings: "Running", "Stopped", "Frozen".
  // Transitional states ("Starting", "Stopping", "Freezing") map to
  // "other" so the filter dropdown keeps them visible to the user.
  if (s === "running" || s.includes("run")) return "running";
  if (s === "stopped" || s.includes("stop")) return "stopped";
  if (s === "frozen" || s.includes("froz") || s.includes("freez")) return "frozen";
  return "other";
}

// ===========================================================================
// WS-25: snapshot + backup + policy hooks.
//
// The snapshot surface reuses the existing useToast + useTranslation
// patterns so toasts + invalidations line up with the rest of the
// compute module. The hooks are tenant-scoped via the queryKey; the
// Header's tenant switcher transparently refetches them.
// ===========================================================================

type Snapshot = components["schemas"]["ComputeSnapshot"];

/**
 * useComputeSnapshots — list of snapshots for an instance.
 *
 * Polls every 10s so a snapshot taken via a schedule shows up without a
 * manual refresh; the cadence is conservative so the listing does not
 * compete with the (more critical) instance-status poll.
 */
export function useComputeSnapshots(
  tenantId: string | null,
  instanceId: string | undefined,
) {
  return useQuery({
    queryKey:
      tenantId && instanceId
        ? queryKeys.compute.snapshots(tenantId, instanceId)
        : ["compute", "snapshots", "disabled"],
    enabled: !!tenantId && !!instanceId,
    refetchInterval: 10_000,
    queryFn: async (): Promise<Snapshot[]> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/compute/instances/{instanceId}/snapshots",
        { params: { path: { instanceId: instanceId! } } },
      );
      if (error || !data) {
        throw new Error(`compute.snapshots.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/**
 * useCreateComputeSnapshot — take a manual snapshot.
 *
 * The mutation invalidates the per-instance snapshot list so the new
 * row appears immediately on success. The toast mirrors the compute
 * module's convention.
 */
export function useCreateComputeSnapshot(tenantId: string | null, instanceId: string) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();
  return useMutation({
    mutationFn: async (input: {
      name: string;
      description?: string;
      stateful?: boolean;
    }): Promise<Snapshot> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/compute/instances/{instanceId}/snapshots",
        {
          params: { path: { instanceId } },
          body: {
            name: input.name,
            description: input.description,
            stateful: input.stateful ?? false,
          },
        },
      );
      if (error || !data) {
        throw new Error(`compute.snapshots.create: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({
          queryKey: queryKeys.compute.snapshots(tenantId, instanceId),
        });
      }
      toast({ title: t("compute.snapshots.mutations.createSuccess") });
    },
    onError: () => {
      toast({ title: t("compute.snapshots.mutations.createError"), variant: "destructive" });
    },
  });
}

/**
 * useDeleteComputeSnapshot — delete a snapshot (soft-delete + Incus
 * delete). Invalidates the snapshot list.
 */
export function useDeleteComputeSnapshot(
  tenantId: string | null,
  instanceId: string,
) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();
  return useMutation({
    mutationFn: async (snapshotId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}",
        { params: { path: { instanceId, snapshotId } } },
      );
      if (error) {
        throw new Error(`compute.snapshots.delete: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId) {
        void qc.invalidateQueries({
          queryKey: queryKeys.compute.snapshots(tenantId, instanceId),
        });
      }
      toast({ title: t("compute.snapshots.mutations.deleteSuccess") });
    },
    onError: () => {
      toast({ title: t("compute.snapshots.mutations.deleteError"), variant: "destructive" });
    },
  });
}

/**
 * useRestoreComputeSnapshot — restore the instance to a snapshot. The
 * instance MUST already exist; Incus does not auto-create it.
 */
export function useRestoreComputeSnapshot(
  tenantId: string | null,
  instanceId: string,
) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();
  return useMutation({
    mutationFn: async (snapshotId: string): Promise<void> => {
      const { error, response } = await apiClient.POST(
        "/api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}/restore",
        { params: { path: { instanceId, snapshotId } } },
      );
      if (error) {
        throw new Error(`compute.snapshots.restore: ${response?.status ?? "network"}`);
      }
    },
    onSuccess: () => {
      if (tenantId) {
        // The restore replaces the instance state; invalidate both the
        // snapshot list (in case the UI caches a "current" marker) and
        // the instance detail (so the cached status reconciles).
        void qc.invalidateQueries({
          queryKey: queryKeys.compute.snapshots(tenantId, instanceId),
        });
        void qc.invalidateQueries({
          queryKey: queryKeys.compute.instance(tenantId, instanceId),
        });
      }
      toast({ title: t("compute.snapshots.mutations.restoreSuccess") });
    },
    onError: () => {
      toast({ title: t("compute.snapshots.mutations.restoreError"), variant: "destructive" });
    },
  });
}
