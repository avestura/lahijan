/**
 * /compute/$id — the instance detail page.
 *
 * Header: name + lifecycle buttons (start/stop/restart/freeze/unfreeze/delete),
 * gated by usePerm(). Body: tabbed panel with Overview, Console,
 * Snapshots, Network, Storage, Config, Audit. Auto-refreshes the
 * instance state every 5s (faster while transitioning).
 */
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  PlayIcon,
  RotateCwIcon,
  SquareIcon,
  SnowflakeIcon,
  FlameIcon,
  Trash2Icon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import {
  useComputeInstance,
  useDeleteInstance,
  useLifecycle,
  classifyStatus,
} from "@/features/compute/api";
import { InstanceStatusBadge } from "@/features/compute/components/InstanceStatusBadge";
import { InstanceOverview } from "@/features/compute/components/InstanceOverview";
import { InstanceConsole } from "@/features/compute/components/InstanceConsole";
import { InstanceSnapshots } from "@/features/compute/components/InstanceSnapshots";
import { InstanceNetwork } from "@/features/compute/components/InstanceNetwork";
import { InstanceStorage } from "@/features/compute/components/InstanceStorage";
import { InstanceConfig } from "@/features/compute/components/InstanceConfig";
import { InstanceAudit } from "@/features/compute/components/InstanceAudit";

export const Route = createFileRoute("/compute/$id")({
  component: InstanceDetailPage,
});

const routeApi = getRouteApi("/compute/$id");

/**
 * useInstanceId — typed wrapper around the route's useParams().
 *
 * `routeApi.useParams()` is typed correctly for tsc but eslint's
 * typed-rules program can't see the gen-file augmentation, so the raw
 * call returns `any` from the linter's perspective. Casting through
 * `unknown` keeps the typed lint rules happy without disabling any.
 */
function useInstanceId(): string {
  const params = routeApi.useParams() as unknown as { id: string };
  return params.id;
}

// Tab identifiers used internally; localized labels come from
// `compute.detail.tabs.<id>`.
const DETAIL_TABS = [
  "overview",
  "console",
  "snapshots",
  "network",
  "storage",
  "config",
  "audit",
] as const;

type DetailTab = (typeof DETAIL_TABS)[number];

function InstanceDetailPage() {
  const { t } = useTranslation();
  // The lint rule sees the routeApi.useParams() return as `any` because
  // routeTree.gen.ts is excluded from the tsconfig project (its
  // `declare module` augmentation only kicks in for tsc, not for the
  // eslint typed-rules' program). We use a typed wrapper that mirrors
  // the route's params shape explicitly so the typed lint rules see a
  // concrete type.
  const id = useInstanceId();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const query = useComputeInstance(tenantId, id);
  const lifecycle = useLifecycle(tenantId);
  const destroy = useDeleteInstance(tenantId);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [forceDelete, setForceDelete] = useState(false);

  const { hasPerm: canStart } = usePerm("compute.instance.start");
  const { hasPerm: canStop } = usePerm("compute.instance.stop");
  const { hasPerm: canRestart } = usePerm("compute.instance.restart");
  const { hasPerm: canDelete } = usePerm("compute.instance.delete");

  if (query.isLoading) {
    return <LoadingState rows={4} />;
  }
  if (query.error || !query.data) {
    return (
      <ErrorState
        message={t("compute.detail.notFound")}
        retryLabel={t("common.retry")}
        onRetry={() => void query.refetch()}
      />
    );
  }

  const inst = query.data;
  const bucket = classifyStatus(inst.status);
  const canRunNow = bucket === "stopped";
  const canStopNow = bucket === "running";
  const canFreezeNow = bucket === "running";
  const canUnfreezeNow = bucket === "frozen";

  const onDelete = () => {
    destroy.mutate(
      { instanceId: inst.id, force: forceDelete },
      {
        onSettled: () => {
          setDeleteOpen(false);
        },
      },
    );
  };

  const renderTab = (tab: DetailTab) => {
    switch (tab) {
      case "overview":
        return <InstanceOverview instance={inst} />;
      case "console":
        return <InstanceConsole instanceId={inst.id} status={inst.status} />;
      case "snapshots":
        return <InstanceSnapshots />;
      case "network":
        return <InstanceNetwork instance={inst} />;
      case "storage":
        return <InstanceStorage instance={inst} />;
      case "config":
        return <InstanceConfig instance={inst} />;
      case "audit":
        return <InstanceAudit instanceId={inst.id} />;
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <Link
          to="/compute"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeftIcon className="h-4 w-4" />
          {t("compute.title")}
        </Link>
      </div>

      <header className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-1">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold">{inst.name}</h1>
            <InstanceStatusBadge status={inst.status} />
          </div>
          {inst.description ? (
            <p className="text-sm text-muted-foreground">{inst.description}</p>
          ) : null}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {canStart && canRunNow && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => lifecycle.mutate({ instanceId: inst.id, action: "start" })}
            >
              <PlayIcon className="h-4 w-4" />
              {t("compute.actions.start")}
            </Button>
          )}
          {canStop && canStopNow && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => lifecycle.mutate({ instanceId: inst.id, action: "stop" })}
            >
              <SquareIcon className="h-4 w-4" />
              {t("compute.actions.stop")}
            </Button>
          )}
          {canRestart && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => lifecycle.mutate({ instanceId: inst.id, action: "restart" })}
            >
              <RotateCwIcon className="h-4 w-4" />
              {t("compute.actions.restart")}
            </Button>
          )}
          {canStop && canFreezeNow && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => lifecycle.mutate({ instanceId: inst.id, action: "freeze" })}
            >
              <SnowflakeIcon className="h-4 w-4" />
              {t("compute.actions.freeze")}
            </Button>
          )}
          {canStart && canUnfreezeNow && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => lifecycle.mutate({ instanceId: inst.id, action: "unfreeze" })}
            >
              <FlameIcon className="h-4 w-4" />
              {t("compute.actions.unfreeze")}
            </Button>
          )}
          {canDelete && (
            <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
              <Trash2Icon className="h-4 w-4" />
              {t("compute.actions.delete")}
            </Button>
          )}
        </div>
      </header>

      <Tabs defaultValue={DETAIL_TABS[0]} className="w-full">
        <TabsList className="flex-wrap">
          {DETAIL_TABS.map((tab) => (
            <TabsTrigger key={tab} value={tab}>
              {t(`compute.detail.tabs.${tab}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {DETAIL_TABS.map((tab) => (
          <TabsContent key={tab} value={tab}>
            {renderTab(tab)}
          </TabsContent>
        ))}
      </Tabs>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("compute.deleteConfirm.title")}</DialogTitle>
            <DialogDescription>{t("compute.deleteConfirm.body")}</DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <Checkbox
              id="force-delete"
              checked={forceDelete}
              onCheckedChange={(v) => setForceDelete(v === true)}
            />
            <Label htmlFor="force-delete">{t("compute.deleteConfirm.force")}</Label>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={onDelete} disabled={destroy.isPending}>
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
